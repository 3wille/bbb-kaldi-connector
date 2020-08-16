package main

import (
	"bytes"
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
	room := "echo87628"
	sipURL := url.URL{Scheme: "wss", Host: host, Path: "/ws", RawQuery: "sessionToken=pjenpwizqvoqfxa8"}
	sipConnection, _, err := websocket.DefaultDialer.Dial(sipURL.String(), nil)
	if err != nil {
		log.Fatal("sip dial:", err)
	}
	defer sipConnection.Close()

	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			_, message, err := sipConnection.ReadMessage()
			if err != nil {
				log.Println("read:", err)
				return
			}
			log.Printf("recv: %s", message)
		}
	}()

	// Connect to the remote SIP UDP endpoint.
	// raddr := sipConnection.RemoteAddr().(*net.UDPAddr)
	// laddr := sipConnection.LocalAddr().(*net.UDPAddr)

	// Create an RTP socket.
	rtpsock, err := net.ListenPacket("udp", "0.0.0.0:0")
	if err != nil {
		log.Fatal("rtp listen:", err)
		return
	}
	defer rtpsock.Close()
	rtpaddr := rtpsock.LocalAddr().(*net.UDPAddr)

	// Create an invite message and attach the SDP.
	invite := &sip.Msg{
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
		// Via: &sip.Via{
		// 	Host:  laddr.IP.String(),
		// 	Port:  uint16(laddr.Port),
		// 	Param: &sip.Param{Name: "branch", Value: util.GenerateBranch()},
		// },
		From: &sip.Addr{
			Display: "SIP Test",
			Uri: &sip.URI{
				Scheme: "sip",
				User:   "w_bsauub1dfst0_7-bbbID-Frederik%20Wille",
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
		// Contact: &sip.Addr{
		// 	Uri: &sip.URI{
		// 		Scheme: "sip",
		// 		Host:   laddr.IP.String(),
		// 		Port:   uint16(laddr.Port),
		// 	},
		// },
		Allow:     "ACK,CANCEL,INVITE,MESSAGE,BYE,OPTIONS,INFO,NOTIFY,REFER",
		UserAgent: "gosip/1.o",
		Payload:   sdp.New(rtpaddr, sdp.Opus),
	}

	// Turn invite message into a packet and send via UDP socket.
	var b bytes.Buffer
	invite.Append(&b)
	// if amt, err := sipConnection.Write(b.Bytes()); err != nil || amt != b.Len() {
	// 	t.Fatal(err)
	// }
	err = sipConnection.WriteMessage(websocket.TextMessage, b.Bytes())
	if err != nil {
		log.Fatal("send sip over websocket: ", err)
		return
	}
	log.Printf(">>>\n%s\n", b.String())

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

func main() {
	log.Println("Starting")
	sipCall()
}
