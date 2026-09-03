package signaling

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/open-carrier-network/ocn/internal/auth"
	"github.com/open-carrier-network/ocn/internal/numbers"
	"github.com/open-carrier-network/ocn/internal/services"
	"github.com/open-carrier-network/ocn/internal/store"
	"github.com/pion/webrtc/v3"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Client struct {
	conn   *websocket.Conn
	user   *store.User
	sendCh chan []byte
	callID string
	mu     sync.Mutex
}

type Server struct {
	store      *store.Store
	auth       *auth.AuthManager
	allocator  *numbers.Allocator
	areaCode   string
	clients    map[string]*Client // key: phone number
	services   *services.Registry
	svcCalls   map[string]*serviceCall // key: callID
	mu         sync.RWMutex
}

type serviceCall struct {
	client  *Client
	service services.Service
}

func NewServer(s *store.Store, a *auth.AuthManager, alloc *numbers.Allocator, areaCode string, reg *services.Registry) *Server {
	return &Server{
		store:     s,
		auth:      a,
		allocator: alloc,
		areaCode:  areaCode,
		clients:   make(map[string]*Client),
		services:  reg,
		svcCalls:  make(map[string]*serviceCall),
	}
}

// HandleWebSocket handles incoming WebSocket connections
func (srv *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	client := &Client{
		conn:   conn,
		sendCh: make(chan []byte, 64),
	}

	go srv.readPump(client)
	go srv.writePump(client)
}

func (srv *Server) readPump(client *Client) {
	defer func() {
		srv.unregisterClient(client)
		client.conn.Close()
	}()

	for {
		_, data, err := client.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		var msg ClientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			srv.sendError(client, 400, "invalid message format")
			continue
		}

		srv.handleMessage(client, &msg)
	}
}

func (srv *Server) writePump(client *Client) {
	defer client.conn.Close()

	for msg := range client.sendCh {
		if err := client.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			break
		}
	}
}

func (srv *Server) handleMessage(client *Client, msg *ClientMessage) {
	switch {
	case msg.ChallengeRequest != nil:
		log.Printf("Received challenge_request")
		srv.handleChallengeRequest(client, msg.ChallengeRequest)
	case msg.Register != nil:
		log.Printf("Received register")
		srv.handleRegister(client, msg.Register)
	case msg.Call != nil:
		log.Printf("Received call to %s", msg.Call.Destination)
		srv.handleCall(client, msg.Call)
	case msg.CallAnswer != nil:
		log.Printf("Received call_answer for %s", msg.CallAnswer.CallID)
		srv.handleCallAnswer(client, msg.CallAnswer)
	case msg.CallHangup != nil:
		log.Printf("Received call_hangup for %s", msg.CallHangup.CallID)
		srv.handleCallHangup(client, msg.CallHangup)
	case msg.ICECandidate != nil:
		srv.handleICECandidate(client, msg.ICECandidate)
	case msg.Ping != nil:
		srv.sendMessage(client, &ServerMessage{Pong: &Pong{}})
	default:
		log.Printf("Received unknown message type")
		srv.sendError(client, 400, "unknown message type")
	}
}

func (srv *Server) handleChallengeRequest(client *Client, msg *ChallengeRequest) {
	if msg.KsimID == nil || len(msg.KsimID.PublicKey) == 0 {
		srv.sendError(client, 400, "missing ksim_id")
		return
	}

	nonce, timestamp, err := srv.auth.GenerateChallenge(msg.KsimID.PublicKey)
	if err != nil {
		srv.sendError(client, 500, "failed to generate challenge")
		return
	}

	srv.sendMessage(client, &ServerMessage{
		ChallengeResponse: &ChallengeResponseMsg{
			Nonce:     nonce,
			Timestamp: timestamp,
		},
	})
}

func (srv *Server) handleRegister(client *Client, msg *RegisterRequest) {
	if msg.KsimID == nil || msg.ChallengeResponse == nil {
		srv.sendError(client, 400, "missing ksim_id or challenge_response")
		return
	}

	err := srv.auth.VerifyResponse(msg.KsimID.PublicKey, msg.ChallengeResponse.Signature)
	if err != nil {
		srv.sendError(client, 401, "authentication failed: "+err.Error())
		return
	}

	user, err := srv.store.GetUserByPublicKey(msg.KsimID.PublicKey)
	if err != nil {
		srv.sendError(client, 500, "database error")
		return
	}

	displayName := ""
	if msg.DisplayName != nil {
		displayName = msg.DisplayName.Name
	}

	if user == nil {
		num, err := srv.allocator.Allocate()
		if err != nil {
			srv.sendError(client, 500, "failed to allocate number")
			return
		}

		user = &store.User{
			KSimPublicKey: msg.KsimID.PublicKey,
			AreaCode:      srv.areaCode,
			Number:        num,
			DisplayName:   displayName,
			RegisteredAt:  time.Now(),
			LastSeen:      time.Now(),
		}
		if err := srv.store.CreateUser(user); err != nil {
			srv.sendError(client, 500, "failed to register user")
			return
		}
	} else {
		if displayName != "" {
			srv.store.UpdateDisplayName(msg.KsimID.PublicKey, displayName)
			user.DisplayName = displayName
		}
		srv.store.UpdateLastSeen(msg.KsimID.PublicKey)
	}

	srv.mu.Lock()
	srv.clients[user.Number] = client
	srv.mu.Unlock()
	client.user = user

	srv.sendMessage(client, &ServerMessage{
		RegisterResponse: &RegisterResponse{
			Success: true,
			AssignedNumber: &PhoneNumber{
				AreaCode: user.AreaCode,
				Number:   user.Number,
			},
		},
	})

	log.Printf("User registered: %s (%s)", numbers.FormatNumber(user.AreaCode, user.Number), user.DisplayName)
}

func (srv *Server) handleCall(client *Client, msg *CallRequest) {
	if client.user == nil {
		srv.sendError(client, 401, "not registered")
		return
	}

	log.Printf("Call from %s to %s", client.user.Number, msg.Destination)

	// Check if destination is a service code (*XX)
	if srv.services.IsServiceCode(msg.Destination) {
		srv.handleServiceCall(client, msg)
		return
	}

	areaCode, localNum, err := numbers.ParseNumber(msg.Destination, srv.areaCode)
	if err != nil {
		log.Printf("Call failed: invalid destination: %v", err)
		srv.sendError(client, 400, "invalid destination: "+err.Error())
		return
	}

	log.Printf("Parsed: areaCode=%q localNum=%q (server areaCode=%q)", areaCode, localNum, srv.areaCode)

	// If we have an area code and it doesn't match, it's a cross-server call
	if srv.areaCode != "" && areaCode != srv.areaCode {
		srv.sendError(client, 501, "cross-server calls not yet implemented")
		return
	}

	srv.mu.RLock()
	calleeClient, online := srv.clients[localNum]
	srv.mu.RUnlock()

	log.Printf("Callee %s online: %v", localNum, online)

	if !online {
		srv.sendError(client, 404, "user not online")
		return
	}

	callID := generateCallID()
	log.Printf("Sending incoming_call %s to callee %s", callID, localNum)

	srv.sendMessage(calleeClient, &ServerMessage{
		IncomingCall: &IncomingCall{
			CallID: callID,
			CallerNumber: &PhoneNumber{
				AreaCode: client.user.AreaCode,
				Number:   client.user.Number,
			},
			CallerName: &DisplayName{Name: client.user.DisplayName},
			Offer:      msg.Offer,
		},
	})

	client.mu.Lock()
	client.callID = callID
	calleeClient.mu.Lock()
	calleeClient.callID = callID
	calleeClient.mu.Unlock()
	client.mu.Unlock()

	srv.sendMessage(client, &ServerMessage{
		CallRinging: &CallRinging{CallID: callID},
	})

	log.Printf("Call %s established between %s and %s", callID, client.user.Number, localNum)
}

func (srv *Server) handleServiceCall(client *Client, msg *CallRequest) {
	svc, err := srv.services.Get(msg.Destination)
	if err != nil {
		srv.sendError(client, 404, "service not found")
		return
	}

	callID := generateCallID()
	log.Printf("Service call %s to %s (%s)", callID, msg.Destination, svc.Name())

	// Convert SDP
	offer := &webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  msg.Offer.SDP,
	}

	// Callback to send service's ICE candidates back to the caller
	sendICE := func(candidate webrtc.ICECandidateInit) {
		var mid string
		if candidate.SDPMid != nil {
			mid = *candidate.SDPMid
		}
		var mline int32
		if candidate.SDPMLineIndex != nil {
			mline = int32(*candidate.SDPMLineIndex)
		}
		srv.sendMessage(client, &ServerMessage{
			ICECandidate: &ICECandidateTrickle{
				CallID: callID,
				Candidate: &ICECandidate{
					Candidate:     candidate.Candidate,
					SDPMid:        mid,
					SDPMLineIndex: mline,
				},
			},
		})
	}

	// Let the service handle the call
	answer, err := svc.HandleCall(callID, offer, sendICE)
	if err != nil {
		log.Printf("Service call failed: %v", err)
		srv.sendError(client, 500, "service error: "+err.Error())
		return
	}

	// Track the service call
	srv.mu.Lock()
	srv.svcCalls[callID] = &serviceCall{client: client, service: svc}
	client.callID = callID
	srv.mu.Unlock()

	// Send answer to caller with service info
	srv.sendMessage(client, &ServerMessage{
		CallConnected: &CallConnected{
			CallID: callID,
			Answer: &SDPSession{
				SDP:  answer.SDP,
				Type: "answer",
			},
			Service: &ServiceInfo{
				Code: svc.Code(),
				Name: svc.Name(),
			},
		},
	})

	log.Printf("Service call %s connected: %s", callID, svc.Name())
}

func (srv *Server) handleCallAnswer(client *Client, msg *CallAnswer) {
	if client.user == nil {
		srv.sendError(client, 401, "not registered")
		return
	}

	log.Printf("Call answer from %s for call %s", client.user.Number, msg.CallID)

	srv.mu.RLock()
	var callerClient *Client
	for _, c := range srv.clients {
		c.mu.Lock()
		if c.callID == msg.CallID && c != client {
			callerClient = c
		}
		c.mu.Unlock()
	}
	srv.mu.RUnlock()

	if callerClient == nil {
		log.Printf("Call %s not found for answer", msg.CallID)
		srv.sendError(client, 404, "call not found")
		return
	}

	log.Printf("Forwarding answer to caller %s", callerClient.user.Number)

	// Forward the SDP answer to the caller
	srv.sendMessage(callerClient, &ServerMessage{
		CallConnected: &CallConnected{
			CallID: msg.CallID,
			Answer: msg.Answer,
		},
	})
}

func (srv *Server) handleCallHangup(client *Client, msg *CallHangup) {
	if client.user == nil {
		return
	}

	log.Printf("Hangup request from %s (server callID=%s, client callID=%s)", client.user.Number, client.callID, msg.CallID)

	client.mu.Lock()
	callID := client.callID
	client.callID = ""
	client.mu.Unlock()

	if callID == "" {
		log.Printf("No active call to hangup for %s", client.user.Number)
		return
	}

	// Check if this is a service call
	srv.mu.Lock()
	if svcCall, ok := srv.svcCalls[callID]; ok {
		delete(srv.svcCalls, callID)
		srv.mu.Unlock()
		log.Printf("Ending service call %s", callID)
		svcCall.service.EndCall(callID)
		return
	}
	srv.mu.Unlock()

	srv.mu.RLock()
	for _, c := range srv.clients {
		c.mu.Lock()
		if c.callID == callID && c != client {
			log.Printf("Sending call_ended to %s for call %s", c.user.Number, callID)
			c.callID = ""
			srv.sendMessage(c, &ServerMessage{
				CallEnded: &CallEnded{CallID: callID, Reason: "hangup"},
			})
		}
		c.mu.Unlock()
	}
	srv.mu.RUnlock()
}

func (srv *Server) handleICECandidate(client *Client, msg *ICECandidateTrickle) {
	if client.user == nil {
		return
	}

	// Check if this is a service call
	srv.mu.RLock()
	svcCall, isService := srv.svcCalls[msg.CallID]
	srv.mu.RUnlock()

	if isService {
		// Forward ICE candidate to the service
		mid := msg.Candidate.SDPMid
		mline := uint16(msg.Candidate.SDPMLineIndex)
		candidate := webrtc.ICECandidateInit{
			Candidate:     msg.Candidate.Candidate,
			SDPMid:        &mid,
			SDPMLineIndex: &mline,
		}
		if err := svcCall.service.HandleICE(candidate); err != nil {
			log.Printf("Failed to add ICE candidate to service: %v", err)
		}
		return
	}

	// Regular call - forward to other client
	srv.mu.RLock()
	for _, c := range srv.clients {
		c.mu.Lock()
		if c.callID == msg.CallID && c != client {
			srv.sendMessage(c, &ServerMessage{
				ICECandidate: msg,
			})
		}
		c.mu.Unlock()
	}
	srv.mu.RUnlock()
}

func (srv *Server) unregisterClient(client *Client) {
	if client.user == nil {
		return
	}

	client.mu.Lock()
	callID := client.callID
	client.callID = ""
	client.mu.Unlock()

	if callID != "" {
		srv.mu.RLock()
		for _, c := range srv.clients {
			c.mu.Lock()
			if c.callID == callID && c != client {
				c.callID = ""
				srv.sendMessage(c, &ServerMessage{
					CallEnded: &CallEnded{CallID: callID, Reason: "disconnect"},
				})
			}
			c.mu.Unlock()
		}
		srv.mu.RUnlock()
	}

	srv.mu.Lock()
	delete(srv.clients, client.user.Number)
	srv.mu.Unlock()

	log.Printf("User disconnected: %s-%s", client.user.AreaCode, client.user.Number)
}

func (srv *Server) sendMessage(client *Client, msg *ServerMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling message: %v", err)
		return
	}
	select {
	case client.sendCh <- data:
	default:
		log.Printf("Client send buffer full, dropping message")
	}
}

func (srv *Server) sendError(client *Client, code int, message string) {
	srv.sendMessage(client, &ServerMessage{
		Error: &Error{Code: int32(code), Message: message},
	})
}

func generateCallID() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}
