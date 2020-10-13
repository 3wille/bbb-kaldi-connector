package main

import (
	"bbb-kaldi-connector/bbb"
	"context"
	"encoding/json"
	"log"
	"os"
	"regexp"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/gomodule/redigo/redis"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	err := sentry.Init(sentry.ClientOptions{
		Dsn: os.Args[1],
	})
	if err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}

	host := "127.0.0.1"
	log.Println("Setting up redis connection")
	redisConnection := bbb.NewRedisConnection(host)
	channels := []string{"asr_text_*"}
	pubSubConn := redis.PubSubConn{Conn: redisConnection}
	err = pubSubConn.PSubscribe(redis.Args{}.AddFlat(channels)...)
	if err != nil {
		log.Fatal("Couldn't subscribe to BBB channels: ", err)
	}
	log.Print("Subscribed to channels")
	for {
		switch message := pubSubConn.Receive().(type) {
		case redis.Message:
			processMessage(message)
		case redis.Subscription:
			log.Printf("%s: %s %d\n", message.Channel, message.Kind, message.Count)
		case error:
			log.Fatal(message)
		}
	}
}

func processMessage(message redis.Message) {
	messageData := parseMessage(message)
	if messageData.Handle == "completeUtterance" {
		channelRegex := regexp.MustCompile(`asr_text_(\w*-\w*)`)
		channel := message.Channel
		matches := channelRegex.FindStringSubmatch(channel)
		meetingID := matches[1]
		log.Printf("%q\n", matches)
		log.Print(messageData)
		log.Print(meetingID)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
		defer func() {
			if err = client.Disconnect(ctx); err != nil {
				panic(err)
			}
		}()
		err = client.Ping(ctx, readpref.Primary())

		collection := client.Database("meteor").Collection("captions")
		filter := bson.M{"meetingId": meetingID}
		result := collection.FindOneAndUpdate(ctx, filter, bson.M{"data": messageData.Utterance})
		if result.Err() != nil {
			log.Print(result.Err())
		}
		log.Print(result)
	}
}

func parseMessage(v redis.Message) (message kaldiRedisMessage) {
	json.Unmarshal(v.Data, &message)
	return
}

type kaldiRedisMessage struct {
	Handle    string `json:"handle"`
	Utterance string `json:"utterance"`
	Key       string `json:"key"`
	Speaker   string `json:"speaker"`
}
