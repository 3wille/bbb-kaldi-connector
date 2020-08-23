package main

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	// "github.com/jart/gosip/rtp"
	"github.com/jart/gosip/sdp"
	"github.com/jart/gosip/sip"
	"github.com/jart/gosip/util"
)

// log.Fatal(http.ListenAndServe(":8000", nil))

func sipCall() {
	host := "ltbbb1.informatik.uni-hamburg.de"
	room := "echo72278"
	sessionToken := "wed7bgjka4kslkn4"
	sipURL := url.URL{Scheme: "wss", Host: host, Path: "/ws", RawQuery: fmt.Sprintf("sessionToken=%v", sessionToken)}
	log.Print(sipURL.String())
	sipConnection, _, err := websocket.DefaultDialer.Dial(sipURL.String(), nil)
	if err != nil {
		log.Fatal("sip dial: ", err)
	}
	defer sipConnection.Close()

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
	rtpsock, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		log.Fatal("rtp listen:", err)
		return
	}
	defer rtpsock.Close()
	rtpaddr := rtpsock.LocalAddr().(*net.UDPAddr)
	log.Print(rtpaddr)

	// Create an invite message and attach the SDP.
	invite := buildSipInvite(room, host, rtpaddr)

	sendSipInvite(invite, sipConnection)

	waitFor100Trying(sipConnection)

	// Receive 200 OK.
	sipConnection.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, message, err := sipConnection.ReadMessage()
	if err != nil {
		log.Fatal("read 200 ok:", err)
	}
	log.Printf("<<< \n%s\n", string(message))
	msg, err := sip.ParseMsg(message)
	if err != nil {
		log.Fatal("parse 200 ok:", err)
	}
	if !msg.IsResponse() || msg.Status != 200 || msg.Phrase != "OK" {
		log.Fatal("wanted 200 ok but got:", msg.Status, msg.Phrase)
	}

	// Figure out where they want us to send RTP.
	var rrtpaddr *net.UDPAddr
	if ms, ok := msg.Payload.(*sdp.SDP); ok {
		rrtpaddr = &net.UDPAddr{IP: net.ParseIP(ms.Addr), Port: int(ms.Audio.Port)}
	} else {
		log.Fatal("200 ok didn't have sdp payload")
	}
	log.Print(rrtpaddr)

	// Acknowledge the 200 OK to answer the call.
	var ack sip.Msg
	ack.Request = invite.Request
	ack.From = msg.From
	ack.To = msg.To
	ack.CallID = msg.CallID
	ack.Method = "ACK"
	ack.CSeq = msg.CSeq
	ack.CSeqMethod = "ACK"
	ack.Via = msg.Via
	var b bytes.Buffer
	ack.Append(&b)
	err = sipConnection.WriteMessage(websocket.TextMessage, b.Bytes())
	if err != nil {
		log.Fatal("send ack over websocket: ", err)
	}

	var bye sip.Msg
	bye.Method = "BYE"
	bye.CSeqMethod = "BYE"
	bye.CSeq++
	b.Reset()
	bye.Append(&b)
	err = sipConnection.WriteMessage(websocket.TextMessage, b.Bytes())
	if err != nil {
		log.Fatal("send ack over websocket: ", err)
	}

	// Wait for acknowledgment of hangup.
	sipConnection.SetReadDeadline(time.Now().Add(time.Second * 2))
	_, message, err = sipConnection.ReadMessage()
	if err != nil {
		log.Fatal(err)
	}
	msg, err = sip.ParseMsg(message)
	if err != nil {
		log.Fatal(err)
	}
	if !msg.IsResponse() || msg.Status != 200 || msg.Phrase != "OK" {
		log.Fatal("wanted bye response 200 ok but got:", msg.Status, msg.Phrase)
	}
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
	return &sip.Msg{
		CallID:     util.GenerateCallID(),
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
			Host:      "mjddf3j4c6bs.invalid",
			// Port:  uint16(laddr.Port),
			Param: &sip.Param{Name: "branch", Value: util.GenerateBranch()},
		},
		From: &sip.Addr{
			Display: "SIP Test",
			Uri: &sip.URI{
				Scheme: "sip",
				User:   "w_bsaucdftt0_7-bbbID-Frederik%20Wille",
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
				User:   "3hq5f4mr",
				Host:   "mjdde3j4j6bs.invalid",
				// Host:   laddr.IP.String(),
				// Port:   uint16(laddr.Port),
			},
			Param: &sip.Param{Name: "transport", Value: "ws"},
		},
		Allow:     "ACK,CANCEL,INVITE,MESSAGE,BYE,OPTIONS,INFO,NOTIFY,REFER",
		UserAgent: "gosip/1.o",
		Supported: "outbound",
		Payload:   sdp.New(rtpaddr, sdp.Opus),
	}
}

func main() {
	log.Println("Starting")
	sipCall()
}
