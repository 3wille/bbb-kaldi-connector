package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/3wille/sip-go/wernerd-GoRTP/src/net/rtp"
	"github.com/gomodule/redigo/redis"
	"gopkg.in/hraban/opus.v2"

	"github.com/gorilla/websocket"
	"github.com/jart/gosip/sdp"
	"github.com/jart/gosip/sip"
	"github.com/jart/gosip/util"
	"github.com/thanhpk/randstr"
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

func main() {
	log.Println("Setting up redis connection")
	redisConnection, err := redis.Dial("tcp", "127.0.0.1:6379",
		redis.DialReadTimeout(10*time.Second),
		redis.DialWriteTimeout(10*time.Second))
	if err != nil {
		log.Fatal("Couldn't connect to redis: ", err)
	}
	defer redisConnection.Close()
	log.Print("Connected to redis")
	channels := []string{"to-akka-apps-redis-channel"}
	pubSubConn := redis.PubSubConn{Conn: redisConnection}
	err = pubSubConn.Subscribe(redis.Args{}.AddFlat(channels)...)
	if err != nil {
		log.Fatal("Couldn't subscribe to BBB channels: ", err)
	}
	log.Print("Subscribed to channels")
	for {
		switch v := pubSubConn.Receive().(type) {
		case redis.Message:
			sipExtension, meetingID := parseMeetingDataFromRedisMessage(v)
			if sipExtension == "" {
				continue
			}
			log.Println(meetingID)
			sessionToken := "3wimoyhimqwqqhce"
			go relay(sipExtension, sessionToken)
		// case redis.Subscription:
		// 	fmt.Printf("%s: %s %d\n", v.Channel, v.Kind, v.Count)
		case error:
			log.Print(v)
		}
	}
}

func parseMeetingDataFromRedisMessage(v redis.Message) (sipExtension string, meetingID string) {
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

func relay(room string, sessionToken string) {
	host := "ltbbb1.informatik.uni-hamburg.de"
	secretToken := os.Args[1]
	sipURL := url.URL{Scheme: "wss", Host: host, Path: fmt.Sprintf("ws_%v", secretToken)}
	log.Print(sipURL.String())
	sipConnection, _, err := websocket.DefaultDialer.Dial(sipURL.String(), nil)
	if err != nil {
		log.Fatal("sip dial: ", err)
	}
	stopSignal := make(chan bool, 1)
	startRelay := make(chan bool, 1)

	// done := make(chan struct{})
	// go func() {
	// 	defer close(done)
	// 	for {
	// 		_, message, err := sipConnection.ReadMessage()
	// 		if err != nil {
	// 			log.Println("read:", err)
	// 			return
	// 		}
	// 		log.Printf("recv: %s", message)
	// 	}
	// }()

	// Connect to the remote SIP UDP endpoint.
	// raddr := sipConnection.RemoteAddr().(*net.UDPAddr)
	// laddr := sipConnection.LocalAddr().(*net.UDPAddr)

	// Create an RTP socket.
	rtpsock, err := net.ListenPacket("udp4", "134.100.15.197:4002")
	rtpAddr, _ := net.ResolveIPAddr("ip", "134.100.15.197")
	if err != nil {
		log.Fatal("rtp listen:", err)
		return
	}
	// defer rtpsock.Close()
	rtpUDPAddr := rtpsock.LocalAddr().(*net.UDPAddr)
	log.Print(rtpUDPAddr)

	// Create an invite message and attach the SDP.
	invite := buildSipInvite(room, host, rtpUDPAddr)
	// defer hangup(sipConnection, stopSignal, invite)

	sendSipInvite(invite, sipConnection)

	waitFor100Trying(sipConnection)

	msg := waitFor200Ok(sipConnection)

	// Figure out where they want us to send RTP.
	var remoteRTPAddr *net.UDPAddr
	var pTime int
	if sdpMsg, ok := msg.Payload.(*sdp.SDP); ok {
		remoteRTPAddr = &net.UDPAddr{IP: net.ParseIP(sdpMsg.Addr), Port: int(sdpMsg.Audio.Port)}
		pTime = sdpMsg.Ptime
	} else {
		log.Fatal("200 ok didn't have sdp payload")
	}
	log.Print(remoteRTPAddr)

	rsLocal, err := prepareRTPSession(rtpAddr, rtpUDPAddr, remoteRTPAddr)
	if err != nil {
		hangup(sipConnection, stopSignal, invite)
		log.Fatal("Couldn't create remote: ", err)
	}

	log.Println("Setting up redis connection")
	redisConnection, err := redis.Dial("tcp", "127.0.0.1:6379",
		redis.DialReadTimeout(10*time.Second),
		redis.DialWriteTimeout(10*time.Second))
	if err != nil {
		hangup(sipConnection, stopSignal, invite)
		log.Fatal("Couldn't connect to redis: ", err)
	}
	defer redisConnection.Close()
	log.Print("Connected to redis")
	// channels := []string{"asr", "asr_control"}
	// pubSubConn := redis.PubSubConn{Conn: redisConnection}
	// err = pubSubConn.Subscribe(redis.Args{}.AddFlat(channels)...)
	// if err != nil {
	// 	log.Fatal("Couldn't subscribe to ASR channel: ", err)
	// }
	// log.Print("Subscribed to channels")
	// done := make(chan error, 1)
	// redisConnected := make(chan bool, 1)
	// log.Print("sending status to asr_control")
	// reply, err := redisConnection.Do("PUBLISH", "asr_control", "status")
	// log.Print("converting")
	// var receivedBy int
	// switch reply := reply.(type) {
	// case int64:
	// 	receivedBy = int(reply)
	// }
	// if err != nil {
	// 	hangup(sipConnection, stopSignal, invite)
	// 	log.Fatal("Couldn't publish status:", err)
	// }
	// log.Printf("Send status to %v clients", receivedBy)

	// go func() {
	// 	for {
	// 		switch {
	// 		case <-stopSignal:
	// 			done <- nil
	// 			return
	// 		}
	// 		switch n := pubSubConn.Receive().(type) {
	// 		case error:
	// 			done <- n
	// 			return
	// 		case redis.Message:
	// 			channel := n.Channel
	// 			message := n.Data
	// 			fmt.Printf("channel: %s, message: %s\n", channel, message)
	// 		case redis.Subscription:
	// 			switch n.Count {
	// 			case len(channels):
	// 				// redisConnected <- true
	// 			case 0:
	// 				// Return from the goroutine when all channels are unsubscribed.
	// 				done <- nil
	// 				return
	// 			}
	// 		}
	// 	}
	// }()

	// go receivePacketLocal(rsLocal)
	go func() {
		// Create and store the data receive channel.
		dataReceiver := rsLocal.CreateDataReceiveChan()
		// ctrlReceiver := rsLocal.CreateCtrlEventChan()
		cnt := 0
		wrongSequences := -1 // first package is always wrong bc we can't guess Sequence
		doubleFrames := 0
		skippedFrames := 0
		relayStarted := false

		dec, err := opus.NewDecoder(48000, 1)
		if err != nil {
			hangup(sipConnection, stopSignal, invite)
			log.Fatal("Couldn't open OPUS decoder")
		}

		// Destination file
		debugRecorder, err := os.Create("test.pcm")
		if err != nil {
			hangup(sipConnection, stopSignal, invite)
			log.Fatal(fmt.Sprintf("couldn't create output file - %v", err))
		}

		var lastSequenceNumber uint16
		// var lastTimestamp uint32
		for {
			select {
			case rp := <-dataReceiver:
				if !relayStarted {
					// log.Print("Waiting for 'you are muted' to end")
					continue
				}
				// rp.Print("a")
				payload := rp.Payload()
				newSequenceNumber := rp.Sequence()
				// newTimestamp := rp.Timestamp()
				// log.Print(newTimestamp)
				if newSequenceNumber != lastSequenceNumber+1 {
					wrongSequences++
					if newSequenceNumber == lastSequenceNumber {
						doubleFrames++
					} else if newSequenceNumber == lastSequenceNumber+2 {
						skippedFrames++
					}
				}
				var frameSizeMs = pTime // if you don't know, go with 60 ms.
				channels := 1.0
				sampleRate := 48000.0
				// 48000Hz & 20ms ptime = 960 samples per frame
				frameSize := int(channels * float64(frameSizeMs) * sampleRate / 1000)
				// frameSize := 5760
				pcm := make([]int16, int(frameSize))
				sampleCount, err := dec.Decode(payload, pcm)
				if err != nil {
					log.Println("Couldn't decode frame")
				}
				cnt++
				if cnt%100 == 0 {
					log.Println("Relayed packages: ", cnt)
				}
				pcmBytes := make([]byte, 0, frameSize*2)
				for _, pcmSample := range pcm[:sampleCount] {
					pcmSampleBytes := make([]byte, 2)
					binary.LittleEndian.PutUint16(pcmSampleBytes, uint16(pcmSample))
					pcmBytes = append(pcmBytes, pcmSampleBytes...)
				}
				// log.Println("publishing audio: ", pcmBytes)
				receivedBy, err := redis.Int(redisConnection.Do(
					"PUBLISH", "asr_audio", pcmBytes[:sampleCount*2]))
				_ = receivedBy
				if err != nil {
					log.Println("Couldn't publish audio:", err)
				}
				_, err = debugRecorder.Write(pcmBytes)
				if err != nil {
					hangup(sipConnection, stopSignal, invite)
					log.Print("failed to write debug: ", err)
				}
				// log.Printf("Send audio to %v clients", receivedBy)
				lastSequenceNumber = newSequenceNumber
				rp.FreePacket()
			// case ctrlEvents := <-ctrlReceiver:
			// 	for _, ctrlEvent := range ctrlEvents {
			// 		log.Print(ctrlEvent.EventType)
			// 		log.Print(ctrlEvent.Ssrc)
			// 		log.Print(ctrlEvent.Index)
			// 		log.Print(ctrlEvent.Reason)
			// 	}
			case <-stopSignal:
				log.Println("stop receiving")
				log.Print(wrongSequences)
				log.Print(doubleFrames)
				log.Print(skippedFrames)
				debugRecorder.Close()
				return
			case <-startRelay:
				relayStarted = true
			}
		}
	}()
	rtpsock.Close()
	err = rsLocal.StartSession()
	if err != nil {
		log.Fatal("Couldn't start session: ", err)
	}
	ack200Ok(sipConnection, invite, msg)

	sipChannel := make(chan *sip.Msg, 50)
	go listenForSipMessages(sipConnection, sipChannel, rtpUDPAddr)
	// write example
	go func() {
		const sampleRate = 48000
		const channels = 1 // mono; 2 for stereo

		enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
		if err != nil {
			hangup(sipConnection, stopSignal, invite)
			log.Print("failed to create opus encoder: ", err)
		}
		var pcm []int16 = make([]int16, 960)
		data := make([]byte, 960)
		_, err = enc.Encode(pcm, data)
		if err != nil {
			hangup(sipConnection, stopSignal, invite)
			log.Print("failed to encode pcm: ", err)
		}
		timestamp := uint32(0)
		for {
			time.Sleep(time.Duration(time.Second * 10))
			rp := rsLocal.NewDataPacket(timestamp)
			timestamp += 960
			rp.SetPayload(data)
			rsLocal.WriteData(rp)
			rp.FreePacket()
		}
	}()

	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, os.Interrupt)

	startRelay <- true
	for {
		select {
		case <-sigchan:
			rsLocal.CloseSession()
			hangupFullyEstablished(sipConnection, stopSignal, invite, sipChannel)
			return
		}
	}
	// time.Sleep(5 * time.Second)
	// startRelay <- true
	// time.Sleep(5 * time.Second)
	// rsLocal.CloseSession()
	// hangup(sipConnection, stopSignal, invite)
}

func prepareRTPSession(rtpAddr *net.IPAddr, localRtpUDPAddr *net.UDPAddr, remoteRTPUDPAddr *net.UDPAddr) (rsLocal *rtp.Session, err error) {
	// Create a UDP transport with "local" address and use this for a "local" RTP session
	// The RTP session uses the transport to receive and send RTP packets to the remote peer.
	tpLocal, err := rtp.NewTransportUDP(rtpAddr, localRtpUDPAddr.Port, "")
	if err != nil {
		log.Fatal("Couln't setup TransportUDP: ", err)
	}

	rtp.PayloadFormatMap[111] = &rtp.PayloadFormat{111, rtp.Audio, 48000, 1, "OPUS"}
	// TransportUDP implements TransportWrite and TransportRecv interfaces thus
	// use it as write and read modules for the Session.
	rsLocal = rtp.NewSession(tpLocal, tpLocal)
	strLocalIdx, err := rsLocal.NewSsrcStreamOut(
		&rtp.Address{rtpAddr.IP, localRtpUDPAddr.Port, localRtpUDPAddr.Port + 1, ""}, 0, 0,
	)
	if err.Error() != "" {
		log.Println(err.Error())
		log.Fatal("Couln't setup RTP out outstream: ", err)
	}

	if !rsLocal.SsrcStreamOutForIndex(strLocalIdx).SetPayloadType(111) {
		log.Fatal("Couldn't set payload type")
	}
	_, err = rsLocal.AddRemote(
		&rtp.Address{localRtpUDPAddr.IP, localRtpUDPAddr.Port, localRtpUDPAddr.Port + 1, ""},
	)
	if err != nil {
		log.Fatal("Couln't set up RTP remote: ", err)
	}

	return
}

func listenForSipMessages(sipConnection *websocket.Conn, sipChannel chan *sip.Msg,
	rtpAddr *net.UDPAddr) {
	for {
		sipConnection.SetReadDeadline(time.Time{}) // zero means forever
		_, rawMessage, err := sipConnection.ReadMessage()
		if err != nil {
			log.Fatal("read 200 ok:", err)
		}
		message, err := sip.ParseMsg(rawMessage)
		if err != nil {
			log.Fatal("failed to parse sip message: ", err)
		}
		log.Printf("<<<a \n%s\n", string(rawMessage))
		log.Print("bbb", message.Method)
		if message.Method == "INVITE" {
			// opus := sdp.Codec{PT: 111, Name: "opus", Rate: 48000, Param: "1"}
			var okMessage sip.Msg
			okMessage.Request = message.Request
			okMessage.From = message.To
			okMessage.To = message.From
			okMessage.CallID = message.CallID
			okMessage.Status = 200
			okMessage.Phrase = "OK"
			okMessage.CSeq = message.CSeq
			okMessage.CSeqMethod = message.CSeqMethod
			okMessage.Via = message.Via
			okMessage.XHeader = &sip.XHeader{Name: "Session-Expires", Value: []byte("120;refresher=uas")}
			// okMessage.SessionExpires = message.Session
			// sdpPayload := sdp.New(rtpAddr, opus)
			// sdpPayload.RecvOnly = true
			// okMessage.Payload = sdpPayload
			var b bytes.Buffer
			okMessage.Append(&b)
			err := sipConnection.WriteMessage(websocket.TextMessage, b.Bytes())
			if err != nil {
				log.Fatal("send  over websocket: ", err)
			}
			log.Println(">>> ", b.String())
		} else if message.Method == "BYE" {
			os.Exit(0)
		}
		sipChannel <- message
	}
}

func waitFor200Ok(sipConnection *websocket.Conn) *sip.Msg {
	// Receive 200 OK.
	sipConnection.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, rawMessage, err := sipConnection.ReadMessage()
	if err != nil {
		log.Fatal("read 200 ok:", err)
	}
	log.Printf("<<< \n%s\n", string(rawMessage))
	message, err := sip.ParseMsg(rawMessage)
	if err != nil {
		log.Fatal("parse 200 ok:", err)
	}
	if !message.IsResponse() || message.Status != 200 || message.Phrase != "OK" {
		log.Printf("<<< \n%s\n", string(rawMessage))
		log.Fatal("wanted 200 ok but got:", message.Status, message.Phrase)
	}
	return message
}

func ack200Ok(sipConnection *websocket.Conn, invite *sip.Msg, okMessage *sip.Msg) {
	// Acknowledge the 200 OK to answer the call.
	var ack sip.Msg
	ack.Request = invite.Request
	ack.From = okMessage.From
	ack.To = okMessage.To
	ack.CallID = okMessage.CallID
	ack.Method = "ACK"
	ack.CSeq = okMessage.CSeq
	ack.CSeqMethod = "ACK"
	ack.Via = okMessage.Via
	var b bytes.Buffer
	ack.Append(&b)
	err := sipConnection.WriteMessage(websocket.TextMessage, b.Bytes())
	if err != nil {
		log.Fatal("send ack over websocket: ", err)
	}
}

func hangupFullyEstablished(sipConnection *websocket.Conn, stopLocalRecv chan bool, invite *sip.Msg, sipChannel chan *sip.Msg) {
	stopLocalRecv <- true
	var bye sip.Msg
	bye.Request = invite.Request
	bye.From = invite.From
	bye.To = invite.To
	bye.CallID = invite.CallID
	bye.Via = invite.Via
	bye.Method = "BYE"
	bye.CSeqMethod = "BYE"
	bye.CSeq = util.GenerateCSeq()
	var b bytes.Buffer
	bye.Append(&b)
	err := sipConnection.WriteMessage(websocket.TextMessage, b.Bytes())
	if err != nil {
		log.Fatal("send BYE over websocket: ", err)
	}
	log.Println("send BYE over websocket")
	// log.Println(b.String())

	// Wait for acknowledgment of hangup.
	log.Print("waiting for bye ack")
	sipConnection.SetReadDeadline(time.Now().Add(time.Second * 3))
	_, message, err := sipConnection.ReadMessage()
	if err != nil {
		log.Print(err)
	}
	msg, err := sip.ParseMsg(message)
	if err != nil {
		log.Print(err)
	} else if !msg.IsResponse() || msg.Status != 200 || msg.Phrase != "OK" {
		log.Print("wanted bye response 200 ok but got:", msg.Status, msg.Phrase)
	} else {
		log.Println("Hung up")
	}
	sipConnection.Close()
	// stopLocalRecv <- true
	// os.Exit(0)
}

func hangup(sipConnection *websocket.Conn, stopLocalRecv chan bool, invite *sip.Msg) {
	stopLocalRecv <- true
	var bye sip.Msg
	bye.Request = invite.Request
	bye.From = invite.From
	bye.To = invite.To
	bye.CallID = invite.CallID
	bye.Via = invite.Via
	bye.Method = "BYE"
	bye.CSeqMethod = "BYE"
	bye.CSeq = util.GenerateCSeq()
	var b bytes.Buffer
	bye.Append(&b)
	err := sipConnection.WriteMessage(websocket.TextMessage, b.Bytes())
	if err != nil {
		log.Fatal("send BYE over websocket: ", err)
	}
	log.Println("send BYE over websocket")
	// log.Println(b.String())

	// Wait for acknowledgment of hangup.
	log.Print("waiting for bye ack")
	sipConnection.SetReadDeadline(time.Now().Add(time.Second * 3))
	_, message, err := sipConnection.ReadMessage()
	if err != nil {
		log.Print(err)
	}
	msg, err := sip.ParseMsg(message)
	if err != nil {
		log.Print(err)
	} else if !msg.IsResponse() || msg.Status != 200 || msg.Phrase != "OK" {
		log.Print("wanted bye response 200 ok but got:", msg.Status, msg.Phrase)
	} else {
		log.Println("Hung up")
	}
	sipConnection.Close()
	// stopLocalRecv <- true
	// os.Exit(0)
}

func waitFor100Trying(sipConnection *websocket.Conn) {
	// Receive provisional 100 Trying.
	sipConnection.SetReadDeadline(time.Now().Add(time.Second * 2))
	// memory := make([]byte, 2048)
	_, message, err := sipConnection.ReadMessage()
	// amt, err := conn.Read(memory)
	if err != nil {
		log.Fatal("read 100 trying: ", err)
	}
	log.Printf("<<< %s\n", string(message))
	msg, err := sip.ParseMsg(message)
	if err != nil {
		log.Fatal("parse 100 trying", err)
	}
	if !msg.IsResponse() || msg.Status != 100 || msg.Phrase != "Trying" {
		log.Printf("<<< %s\n", string(message))
		log.Fatal("didn't get 100 trying :[")
	}
}

func sendSipInvite(invite *sip.Msg, sipConnection *websocket.Conn) {
	// Turn invite message into a packet and send via UDP socket.
	var b bytes.Buffer
	invite.Append(&b)
	err := sipConnection.WriteMessage(websocket.TextMessage, b.Bytes())
	if err != nil {
		log.Fatal("send sip over websocket: ", err)
	}
	log.Print("send sip over websocket done")
	log.Printf(">>>\n%s\n", b.String())
}

func buildSipInvite(room string, host string, rtpaddr *net.UDPAddr) *sip.Msg {
	userHost := fmt.Sprintf("%v.invalid", randstr.String(8))
	userID := fmt.Sprintf("%v-SIP-TEST", randstr.String(8))
	codecs := []sdp.Codec{}
	for _, value := range sdp.StandardCodecs {
		codecs = append(codecs, value)
	}
	codecs = append(codecs, sdp.Opus)
	callID := util.GenerateCallID()
	log.Println("CallID: ", callID)
	opus := sdp.Codec{PT: 111, Name: "opus", Rate: 48000, Param: "1"}
	// opus := sdp.Codec{PT: 111, Name: "opus", Rate: 48000, Param: "2"}
	sdpPayload := sdp.New(rtpaddr, opus)
	sdpPayload.RecvOnly = true
	sdpPayload.Attrs = append(
		sdpPayload.Attrs,
		[2]string{"group", "BUNDLE audio"},
		[2]string{"rtcp-mux", ""},
		// [2]string{"rtcp", "9 IN IP4 0.0.0.0"},
		[2]string{"rtcp-fb", "111 transport-cc"},
		// [2]string{"ssrc", "2709942258 cname:gRy8AmT7jkvYCYFz"},
		// [2]string{"ssrc", "2709942258 msid:0hW7O9EcAgjcV0V8jhXoWLUVPbQV44vlznMG 169b5cc7-9b73-43f7-8647-ca23ff71bdd5"},
		// [2]string{"ssrc", "2709942258 mslabel:0hW7O9EcAgjcV0V8jhXoWLUVPbQV44vlznMG"},
		// [2]string{"ssrc", "2709942258 label:169b5cc7-9b73-43f7-8647-ca23ff71bdd5"},
		// [2]string{"extmap", "1 urn:ietf:params:rtp-hdrext:ssrc-audio-level"},
		// [2]string{"extmap", "2 http://www.webrtc.org/experiments/rtp-hdrext/abs-send-time"},
		// [2]string{"extmap", "3 http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01"},
	)
	return &sip.Msg{
		CallID:     callID,
		CSeq:       util.GenerateCSeq(),
		Method:     "INVITE",
		CSeqMethod: "INVITE",
		Request: &sip.URI{
			Scheme: "sip",
			User:   room,
			Host:   host, // raddr.IP.String(),
			// Port:   uint16(raddr.Port),
		},
		Via: &sip.Via{
			Protocol:  "SIP",
			Version:   "2.0",
			Transport: "WSS",
			Host:      userHost,
			// Port:  uint16(laddr.Port),
			Param: &sip.Param{Name: "branch", Value: util.GenerateBranch()},
		},
		From: &sip.Addr{
			Display: fmt.Sprintf("SIP Test %v", randstr.String(2)),
			Uri: &sip.URI{
				Scheme: "sip",
				User:   userID,
				Host:   host, // laddr.IP.String(),
				// Port:   uint16(laddr.Port),
			},
			Param: &sip.Param{
				Name: "tag", Value: util.GenerateTag(),
			},
		},
		To: &sip.Addr{
			Uri: &sip.URI{
				Scheme: "sip",
				User:   room,
				Host:   host, // raddr.IP.String(),
				// Port:   uint16(raddr.Port),
			},
		},
		Contact: &sip.Addr{
			Uri: &sip.URI{
				Scheme: "sip",
				User:   randstr.String(8),
				Host:   userHost,
				// Host:   laddr.IP.String(),
				// Port:   uint16(laddr.Port),
				Param: &sip.URIParam{Name: "ob", Next: &sip.URIParam{Name: "transport", Value: "ws"}},
			},
		},
		// Allow: "ACK,CANCEL,BYE",
		Allow: "ACK,CANCEL,INVITE,MESSAGE,BYE,OPTIONS,INFO,NOTIFY,REFER",
		// UserAgent:   "gosip/1.o",
		UserAgent: "BigBlueButton",
		// Supported:   "outbound",
		// Supported: "",
		// Payload:   sdp.New(rtpaddr, sdp.StandardCodecs[11]),
		// Payload: sdp.New(rtpaddr, codecs...),
		// Payload: sdp.New(rtpaddr, sdp.Opus),
		Payload: sdpPayload, // sdp.New(rtpaddr, opus),
	}
}
