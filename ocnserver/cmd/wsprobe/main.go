// wsprobe is a tiny WebSocket test client used to exercise cross-server calls
// between two running ocnservers. It registers an existing kSIM identity (a
// server.key file) and either places a call or auto-answers incoming calls,
// printing every signaling message it sees.
//
// Usage:
//
//	caller:  wsprobe -ws ws://127.0.0.1:9101/ws -key server.key -mode call -to 3107654321
//	answer:  wsprobe -ws ws://127.0.0.1:9200/ws -key server.key -mode answer
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"log"
	"time"

	"github.com/gorilla/websocket"
	"github.com/open-carrier-network/ocn/internal/ksim"
)

func main() {
	wsURL := flag.String("ws", "", "websocket url")
	keyFile := flag.String("key", "", "path to a kSIM key file (server.key)")
	mode := flag.String("mode", "", "call | answer")
	to := flag.String("to", "", "full destination number for call mode")
	flag.Parse()

	if *wsURL == "" || *keyFile == "" {
		log.Fatal("need -ws and -key")
	}
	k, _, err := ksim.LoadFile(*keyFile, "")
	if err != nil {
		log.Fatal(err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(*wsURL, nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer conn.Close()

	send := func(v interface{}) {
		if err := conn.WriteJSON(v); err != nil {
			log.Fatal("write:", err)
		}
	}

	// challenge_request
	send(map[string]interface{}{
		"challenge_request": map[string]interface{}{
			"ksim_id": map[string]string{"public_key": k.EncodePublicKey()},
		},
	})

	type chResp struct {
		Nonce     []byte `json:"nonce"`
		Timestamp int64  `json:"timestamp"`
	}
	var cr chResp
	if err := conn.ReadJSON(&struct {
		ChallengeResponse *chResp `json:"challenge_response"`
	}{&cr}); err != nil {
		log.Fatal("challenge read:", err)
	}
	sig := k.SignChallenge(cr.Nonce, cr.Timestamp)
	send(map[string]interface{}{
		"register": map[string]interface{}{
			"ksim_id":            map[string]string{"public_key": k.EncodePublicKey()},
			"challenge_response": map[string]string{"signature": base64.StdEncoding.EncodeToString(sig)},
			"display_name":       map[string]string{"name": "wsprobe"},
		},
	})

	// Consume until registered.
	for {
		var m map[string]json.RawMessage
		if err := conn.ReadJSON(&m); err != nil {
			log.Fatal("read:", err)
		}
		if _, ok := m["register_response"]; ok {
			log.Println("registered OK")
			break
		}
		if _, ok := m["error"]; ok {
			log.Printf("register error: %s", m["error"])
			log.Fatal("registration failed")
		}
	}

	done := make(chan struct{})
	go func() {
		for {
			var m map[string]interface{}
			if err := conn.ReadJSON(&m); err != nil {
				log.Println("closed:", err)
				close(done)
				return
			}
			b, _ := json.Marshal(m)
			log.Printf("recv: %s", b)

			// Auto-answer incoming calls with a minimal (dummy) answer so the
			// signaling/bridge path can be validated end to end.
			if *mode == "answer" {
				if ic, ok := m["incoming_call"].(map[string]interface{}); ok {
					callID := ic["call_id"].(string)
					log.Printf("AUTO-ANSWER call %s", callID)
					time.Sleep(300 * time.Millisecond)
					send(map[string]interface{}{
						"call_answer": map[string]interface{}{
							"call_id": callID,
							"answer": map[string]string{
								"sdp":  "v=0\r\no=- 1 1 IN IP4 0.0.0.0\r\ns=-\r\nc=IN IP4 0.0.0.0\r\nt=0 0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\n",
								"type": "answer",
							},
						},
					})
					// Send one fake ICE candidate back.
					send(map[string]interface{}{
						"ice_candidate": map[string]interface{}{
							"call_id": callID,
							"candidate": map[string]interface{}{
								"candidate":       "candidate:1 1 UDP 1 1.2.3.4 5000 typ host",
								"sdp_mid":         "0",
								"sdp_mline_index": 0,
							},
						},
					})
				}
			}
		}
	}()

	if *mode == "call" {
		time.Sleep(500 * time.Millisecond)
		log.Printf("placing call to %s", *to)
		send(map[string]interface{}{
			"call": map[string]interface{}{
				"destination": *to,
				"offer": map[string]string{
					"sdp":  "v=0\r\no=- 1 1 IN IP4 0.0.0.0\r\ns=-\r\nc=IN IP4 0.0.0.0\r\nt=0 0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\n",
					"type": "offer",
				},
			},
		})
		// Send a fake ICE candidate once ringing is seen.
		time.AfterFunc(2*time.Second, func() {
			send(map[string]interface{}{
				"ice_candidate": map[string]interface{}{
					"call_id": "x",
					"candidate": map[string]interface{}{
						"candidate":       "candidate:1 1 UDP 1 9.9.9.9 5000 typ host",
						"sdp_mid":         "0",
						"sdp_mline_index": 0,
					},
				},
			})
		})
	}

	<-done
}
