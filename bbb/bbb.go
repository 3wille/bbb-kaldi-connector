package bbb

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gomodule/redigo/redis"
)

type redisMessage struct {
	Core struct {
		Header struct {
			Name string `json:"name"`
		} `json:"header"`
		Body struct {
			Props struct {
				MeetingProp struct {
					ExtID string `json:"extId"`
				} `json:"meetingProp"`
				VoiceProp struct {
					VoiceConf string `json:"voiceConf"`
				} `json:"voiceProp"`
			} `json:"props"`
		} `json:"body"`
	} `json:"core"`
}

func SetupRedisPubSub() (redis.Conn, redis.PubSubConn) {
	log.Println("Setting up redis connection")
	redisConnection, err := redis.Dial("tcp", "127.0.0.1:6379",
		redis.DialReadTimeout(time.Minute),
		redis.DialWriteTimeout(time.Minute))
	if err != nil {
		log.Fatal("Couldn't connect to redis: ", err)
	}
	log.Print("Connected to redis")
	channels := []string{"to-akka-apps-redis-channel"}
	pubSubConn := redis.PubSubConn{Conn: redisConnection}
	err = pubSubConn.Subscribe(redis.Args{}.AddFlat(channels)...)
	if err != nil {
		log.Fatal("Couldn't subscribe to BBB channels: ", err)
	}
	log.Print("Subscribed to channels")
	return redisConnection, pubSubConn
}

func ParseMeetingDataFromRedisMessage(v redis.Message) (sipExtension string, meetingID string) {
	// fmt.Printf("%s: message: %s\n", v.Channel, v.Data)
	var message redisMessage
	json.Unmarshal(v.Data, &message)
	// fmt.Println(message)
	// log.Println(message)
	if message.Core.Header.Name == "CreateMeetingReqMsg" {
		sipExtension = message.Core.Body.Props.VoiceProp.VoiceConf
		meetingID = message.Core.Body.Props.MeetingProp.ExtID
	}
	return
}
