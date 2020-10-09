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
					IntID string `json:"intId"`
				} `json:"meetingProp"`
				VoiceProp struct {
					VoiceConf string `json:"voiceConf"`
				} `json:"voiceProp"`
			} `json:"props"`
			MeetingID string `json:"meetingId"`
		} `json:"body"`
	} `json:"core"`
}

func SetupRedisPubSub(host string) (redis.Conn, redis.PubSubConn) {
	log.Println("Setting up redis connection")
	redisConnection := NewRedisConnection(host)
	channels := []string{"to-akka-apps-redis-channel"}
	pubSubConn := redis.PubSubConn{Conn: redisConnection}
	err := pubSubConn.Subscribe(redis.Args{}.AddFlat(channels)...)
	if err != nil {
		log.Fatal("Couldn't subscribe to BBB channels: ", err)
	}
	log.Print("Subscribed to channels")
	return redisConnection, pubSubConn
}

func NewRedisConnection(host string) redis.Conn {
	redisConnection, err := redis.Dial("tcp", host+":6379",
		redis.DialReadTimeout(time.Minute),
		redis.DialWriteTimeout(time.Minute))
	if err != nil {
		log.Fatal("Couldn't connect to redis: ", err)
	}
	log.Print("Connected to redis")
	return redisConnection
}

func ParseMessage(v redis.Message) (message redisMessage) {
	json.Unmarshal(v.Data, &message)
	return
}
