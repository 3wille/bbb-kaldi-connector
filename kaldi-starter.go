package main

import (
	"bbb-kaldi-connector/bbb"
	"log"
	"os/exec"
	"syscall"

	"github.com/gomodule/redigo/redis"
)

func main() {
	redisConnection, pubSubConn := bbb.SetupRedisPubSub()
	defer redisConnection.Close()
	kaldiProcessMap := make(map[string]*exec.Cmd)

	for {
		switch v := pubSubConn.Receive().(type) {
		case redis.Message:
			message := bbb.ParseMessage(v)
			if message.Core.Header.Name == "CreateMeetingReqMsg" {
				meetingID := message.Core.Body.Props.MeetingProp.ExtID
				log.Println(meetingID)
				cmd := exec.Command("python", "nnet3_model.py", "-m 2", "-e", "-c 1", "-t", "-r 16000",
					"--yaml-config models/kaldi_tuda_de_nnet3_chain2_new.yaml")
				err := cmd.Start()
				if err != nil {
					log.Fatal(err)
				}
				kaldiProcessMap[meetingID] = cmd
			} else if message.Core.Header.Name == "DestroyMeetingSysCmdMsg" {
				meetingID := message.Core.Body.Props.MeetingProp.ExtID
				cmd, ok := kaldiProcessMap[meetingID]
				if !ok {
					log.Println("no kaldi process found for meeting: ", meetingID)
				}
				log.Println("stopping for meeting: ", meetingID)
				cmd.Process.Signal(syscall.SIGINT)
			}
		// case redis.Subscription:
		// 	fmt.Printf("%s: %s %d\n", v.Channel, v.Kind, v.Count)
		case error:
			log.Fatal(v)
		}
	}
}
