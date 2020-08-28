package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/3wille/sip-go/wernerd-GoRTP/src/net/rtp"

	"github.com/gomodule/redigo/redis"
	"github.com/gorilla/websocket"
	"github.com/jart/gosip/sdp"
	"github.com/jart/gosip/sip"
	"github.com/jart/gosip/util"
	"github.com/thanhpk/randstr"

	"gopkg.in/hraban/opus.v2"
)

func main() {
	host := "ltbbb1.informatik.uni-hamburg.de"
	room := "29981"
	sessionToken := "oz0jlu8khacic1fa"
	sipURL := url.URL{Scheme: "wss", Host: host, Path: "/ws", RawQuery: fmt.Sprintf("sessionToken=%v", sessionToken)}
	log.Print(sipURL.String())
	sipConnection, _, err := websocket.DefaultDialer.Dial(sipURL.String(), nil)
	if err != nil {
		log.Fatal("sip dial: ", err)
	}
	stopSignal := make(chan bool)

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
	rtpsock, err := net.ListenPacket("udp4", "127.0.0.1:4002")
	rtpAddr, _ := net.ResolveIPAddr("ip", "127.0.0.1")
	if err != nil {
		log.Fatal("rtp listen:", err)
		return
	}
	defer rtpsock.Close()
	rtpUDPAddr := rtpsock.LocalAddr().(*net.UDPAddr)
	log.Print(rtpUDPAddr)

	// Create an invite message and attach the SDP.
	invite := buildSipInvite(room, host, rtpUDPAddr)
	defer hangup(sipConnection, stopSignal, invite)

	sendSipInvite(invite, sipConnection)

	waitFor100Trying(sipConnection)

	msg := waitFor200Ok(sipConnection)

	// Figure out where they want us to send RTP.
	var remoteRTPAddr *net.UDPAddr
	if sdpMsg, ok := msg.Payload.(*sdp.SDP); ok {
		remoteRTPAddr = &net.UDPAddr{IP: net.ParseIP(sdpMsg.Addr), Port: int(sdpMsg.Audio.Port)}
	} else {
		log.Fatal("200 ok didn't have sdp payload")
	}
	log.Print(remoteRTPAddr)

	rsLocal, err := prepareRTPSession(rtpAddr, rtpUDPAddr)
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
	log.Print("sending status to asr_control")
	reply, err := redisConnection.Do("PUBLISH", "asr_control", "status")
	log.Print("converting")
	var receivedBy int
	switch reply := reply.(type) {
	case int64:
		receivedBy = int(reply)
	}
	if err != nil {
		hangup(sipConnection, stopSignal, invite)
		log.Fatal("Couldn't publish status:", err)
	}
	log.Printf("Send status to %v clients", receivedBy)

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
		cnt := 0

		dec, err := opus.NewDecoder(48000, 1)
		if err != nil {
			hangup(sipConnection, stopSignal, invite)
			log.Fatal("Couldn't open OPUS decoder")
		}

		// setup interrupt catcher
		// sigchan := make(chan os.Signal, 1)
		// signal.Notify(sigchan, os.Interrupt)
		for {
			select {
			case rp := <-dataReceiver:
				// log.Println("Remote receiver got:", cnt, "packets")
				// log.Println(rp.Payload())
				// log.Println(rp.Sequence())
				// log.Println(rp.Timestamp())
				// log.Println(rp.PayloadType())
				log.Println("Len: ", len(rp.Payload()))
				payload := rp.Payload()
				// var frameSizeMs float64 = 60 // if you don't know, go with 60 ms.
				// channels := 1.0
				// sampleRate := 16000.0
				// frameSize := channels * frameSizeMs * sampleRate / 1000
				frameSize := 1024
				pcm := make([]int16, int(frameSize))
				sampleCount, err := dec.Decode(payload, pcm)
				if err != nil {
					log.Println("Couldn't decode frame")
				}
				log.Println((sampleCount))
				cnt++
				pcmBytes := make([]byte, 0, 2048)
				for _, in := range pcm {
					b := make([]byte, 2)
					binary.LittleEndian.PutUint16(b, uint16(in))
					pcmBytes = append(pcmBytes, b...)
				}
				log.Println("publishing audio: ", pcm)
				receivedBy, err := redis.Int(redisConnection.Do("PUBLISH", "asr_audio", pcmBytes))
				if err != nil {
					log.Println("Couldn't publish audio:", err)
				}
				log.Printf("Send audio to %v client", receivedBy)
				rp.FreePacket()
				// file.Sync()
			case <-stopSignal:
				log.Println("stop receiving")
				return
			}
		}
	}()
	rtpsock.Close()
	err = rsLocal.StartSession()
	if err != nil {
		log.Fatal("Couldn't start session: ", err)
	}
	ack200Ok(sipConnection, invite, msg)

	// write example
	// payloadByteSlice := make([]byte, 2)
	// rp := rsLocal.NewDataPacket(1)
	// rp.SetPayload(payloadByteSlice)
	// rsLocal.WriteData(rp)
	// rp.FreePacket()

	time.Sleep(10 * time.Second)
	log.Print("End")
	rsLocal.CloseSession()
	hangup(sipConnection, stopSignal, invite)
	os.Exit(0)
}

func prepareRTPSession(rtpAddr *net.IPAddr, rtpUDPAddr *net.UDPAddr) (rsLocal *rtp.Session, err error) {
	// Create a UDP transport with "local" address and use this for a "local" RTP session
	// The RTP session uses the transport to receive and send RTP packets to the remote peer.
	tpLocal, err := rtp.NewTransportUDP(rtpAddr, rtpUDPAddr.Port, "")
	if err != nil {
		log.Fatal("Couln't set up TransportUDP")
	}

	rtp.PayloadFormatMap[111] = &rtp.PayloadFormat{111, rtp.Audio, 48000, 1, "OPUS"}
	// TransportUDP implements TransportWrite and TransportRecv interfaces thus
	// use it as write and read modules for the Session.
	rsLocal = rtp.NewSession(tpLocal, tpLocal)
	strLocalIdx, err := rsLocal.NewSsrcStreamOut(
		&rtp.Address{rtpAddr.IP, rtpUDPAddr.Port, rtpUDPAddr.Port + 1, ""}, 0, 0,
	)
	rsLocal.SsrcStreamOutForIndex(strLocalIdx).SetPayloadType(111)
	_, err = rsLocal.AddRemote(
		&rtp.Address{rtpUDPAddr.IP, rtpUDPAddr.Port, rtpUDPAddr.Port + 1, ""},
	)
	return
}

func waitFor200Ok(sipConnection *websocket.Conn) *sip.Msg {
	// Receive 200 OK.
	sipConnection.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, message, err := sipConnection.ReadMessage()
	if err != nil {
		log.Fatal("read 200 ok:", err)
	}
	// log.Printf("<<< \n%s\n", string(message))
	msg, err := sip.ParseMsg(message)
	if err != nil {
		log.Fatal("parse 200 ok:", err)
	}
	if !msg.IsResponse() || msg.Status != 200 || msg.Phrase != "OK" {
		log.Fatal("wanted 200 ok but got:", msg.Status, msg.Phrase)
	}
	return msg
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
	sipConnection.SetReadDeadline(time.Now().Add(time.Second * 3))
	_, message, err := sipConnection.ReadMessage()
	if err != nil {
		log.Fatal(err)
	}
	msg, err := sip.ParseMsg(message)
	if err != nil {
		log.Fatal(err)
	}
	if !msg.IsResponse() || msg.Status != 200 || msg.Phrase != "OK" {
		log.Fatal("wanted bye response 200 ok but got:", msg.Status, msg.Phrase)
	}
	sipConnection.Close()
	log.Println("Hung up")
	stopLocalRecv <- true
	os.Exit(0)
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
	// log.Printf("<<< %s\n", string(message))
	msg, err := sip.ParseMsg(message)
	if err != nil {
		log.Fatal("parse 100 trying", err)
	}
	if !msg.IsResponse() || msg.Status != 100 || msg.Phrase != "Trying" {
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
	// log.Printf(">>>\n%s\n", b.String())
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
			Param: &sip.Param{Name: "tag", Value: util.GenerateTag()},
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
			},
			Param: &sip.Param{Name: "transport", Value: "ws"},
		},
		Allow:     "ACK,CANCEL,INVITE,MESSAGE,BYE,OPTIONS,INFO,NOTIFY,REFER",
		UserAgent: "gosip/1.o",
		Supported: "outbound",
		// Payload:   sdp.New(rtpaddr, sdp.StandardCodecs[11]),
		// Payload: sdp.New(rtpaddr, codecs...),
		// Payload: sdp.New(rtpaddr, sdp.Opus),
		Payload: sdp.New(rtpaddr, opus),
	}
}
