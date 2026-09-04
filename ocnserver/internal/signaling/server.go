package signaling

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/open-carrier-network/ocn/internal/auth"
	"github.com/open-carrier-network/ocn/internal/dm"
	"github.com/open-carrier-network/ocn/internal/numbers"
	"github.com/open-carrier-network/ocn/internal/push"
	"github.com/open-carrier-network/ocn/internal/services"
	"github.com/open-carrier-network/ocn/internal/store"
	"github.com/open-carrier-network/ocn/internal/voicemail"
	ocnserverpb "github.com/open-carrier-network/ocn/proto/ocnserver"
	"github.com/pion/webrtc/v3"
	"google.golang.org/grpc"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// pendingCallTimeout is how long an FCM-woken call stays queued for an offline
// callee before the caller is told the call was not answered.
const pendingCallTimeout = 45 * time.Second

// ringTimeout is how long an online callee may ring before an unanswered call
// is routed into their voicemail.
const ringTimeout = 25 * time.Second

type Client struct {
	conn   *websocket.Conn
	user   *store.User
	sendCh chan []byte
	callID string
	mu     sync.Mutex
}

type Server struct {
	store        *store.Store
	auth         *auth.AuthManager
	allocator    *numbers.Allocator
	areaCode     string
	clients      map[string]*Client // key: phone number
	services     *services.Registry
	svcCalls     map[string]*serviceCall // key: callID
	pendingCalls map[string]*pendingCall // key: callee number (offline, waiting for FCM wake)
	pusher       push.Sender
	iceServers   []IceServer                 // STUN/TURN handed to clients on registration
	netSvcs      map[string]services.Service // hosted 800/900 numbers
	mu           sync.RWMutex

	// Voicemail (nil when disabled/unconfigured)
	vm *voicemail.Manager

	// Direct messaging (nil when disabled/unconfigured)
	dmm *dm.Manager

	// ringStates tracks calls that are ringing an online callee so a
	// no-answer timer (or a decline/disconnect) can redirect to voicemail.
	ringStates map[string]*ringState
	ringMu     sync.Mutex

	// Federation (registry + inter-server calls)
	reg         registryClient // nil when standalone
	insecureFed bool           // plaintext inter-server gRPC (dev)
	gConns      map[string]*grpc.ClientConn
	outLegs     map[string]*outLeg // callID -> this server is the caller, callee remote
	inLegs      map[string]*inLeg  // callID -> this server hosts the callee, caller remote
	fedMu       sync.RWMutex
}

// ringState is one in-progress ring toward an online local callee.
type ringState struct {
	caller     *Client
	callee     *Client
	calleeUser *store.User
	offer      *SDPSession
	timer      *time.Timer
	answered   bool
}

// pendingCall is a call to a callee that was offline. We store it so that when
// the callee reconnects (woken by the FCM push) the call is delivered to them.
// For a cross-server call, remote marks that the caller is a remote server
// reached over an inbound bridge leg (caller == nil).
type pendingCall struct {
	callID    string
	caller    *Client
	remote    bool
	offer     *SDPSession
	createdAt time.Time
}

type serviceCall struct {
	client  *Client
	service services.Service
}

// registryClient is the subset of the registry client the signaling server
// needs for routing.
type registryClient interface {
	Route(ctx context.Context, area string) (string, error)
	ResolveService(ctx context.Context, fullNumber string) (string, error)
}

func NewServer(s *store.Store, a *auth.AuthManager, alloc *numbers.Allocator, areaCode string, reg *services.Registry, pusher push.Sender) *Server {
	srv := &Server{
		store:        s,
		auth:         a,
		allocator:    alloc,
		areaCode:     areaCode,
		clients:      make(map[string]*Client),
		services:     reg,
		svcCalls:     make(map[string]*serviceCall),
		pendingCalls: make(map[string]*pendingCall),
		pusher:       pusher,
		ringStates:   make(map[string]*ringState),
		gConns:       make(map[string]*grpc.ClientConn),
		outLegs:      make(map[string]*outLeg),
		inLegs:       make(map[string]*inLeg),
		netSvcs:      make(map[string]services.Service),
	}
	go srv.expirePendingCalls()
	return srv
}

// SetVoicemail attaches (or detaches, with nil) the voicemail manager.
func (srv *Server) SetVoicemail(vm *voicemail.Manager) {
	srv.mu.Lock()
	srv.vm = vm
	srv.mu.Unlock()
}

func (srv *Server) voicemailManager() *voicemail.Manager {
	srv.mu.RLock()
	defer srv.mu.RUnlock()
	return srv.vm
}

// SetDM attaches (or detaches, with nil) the direct-messaging manager.
func (srv *Server) SetDM(m *dm.Manager) {
	srv.mu.Lock()
	srv.dmm = m
	srv.mu.Unlock()
}

func (srv *Server) dmManager() *dm.Manager {
	srv.mu.RLock()
	defer srv.mu.RUnlock()
	return srv.dmm
}

// SetRegistry attaches a registry client so cross-server calls can be routed.
// Safe to call after startup (hot federation).
func (srv *Server) SetRegistry(reg registryClient) {
	srv.fedMu.Lock()
	srv.reg = reg
	srv.fedMu.Unlock()
}

// SetFedInsecure toggles plaintext inter-server gRPC (local development only).
func (srv *Server) SetFedInsecure(v bool) {
	srv.fedMu.Lock()
	srv.insecureFed = v
	srv.fedMu.Unlock()
}

// SetAreaCode updates the server's area code at runtime (hot federation).
func (srv *Server) SetAreaCode(a string) {
	srv.fedMu.Lock()
	srv.areaCode = a
	srv.fedMu.Unlock()
}

// SetPusher updates the FCM wake-up sender at runtime.
func (srv *Server) SetPusher(p push.Sender) {
	srv.fedMu.Lock()
	srv.pusher = p
	srv.fedMu.Unlock()
}

func (srv *Server) area() string {
	srv.fedMu.RLock()
	defer srv.fedMu.RUnlock()
	return srv.areaCode
}

func (srv *Server) registry() registryClient {
	srv.fedMu.RLock()
	defer srv.fedMu.RUnlock()
	return srv.reg
}

func (srv *Server) pushSender() push.Sender {
	srv.fedMu.RLock()
	defer srv.fedMu.RUnlock()
	return srv.pusher
}

func (srv *Server) fedInsecure() bool {
	srv.fedMu.RLock()
	defer srv.fedMu.RUnlock()
	return srv.insecureFed
}

// SetICEServers configures the STUN/TURN servers handed to clients on
// registration (typically fetched from the registry).
func (srv *Server) SetICEServers(servers []IceServer) {
	srv.fedMu.Lock()
	srv.iceServers = servers
	srv.fedMu.Unlock()
}

func (srv *Server) clientIceServers() []IceServer {
	srv.fedMu.RLock()
	defer srv.fedMu.RUnlock()
	return srv.iceServers
}

// RegisterNetworkService hosts an 800/900 service number on this server.
func (srv *Server) RegisterNetworkService(fullNumber string, svc services.Service) {
	srv.fedMu.Lock()
	srv.netSvcs[fullNumber] = svc
	srv.fedMu.Unlock()
}

func (srv *Server) networkService(fullNumber string) services.Service {
	srv.fedMu.RLock()
	defer srv.fedMu.RUnlock()
	return srv.netSvcs[fullNumber]
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
	case msg.RegisterFCM != nil:
		srv.handleRegisterFCM(client, msg.RegisterFCM)
	case msg.Call != nil:
		log.Printf("Received call to %s", msg.Call.Destination)
		srv.handleCall(client, msg.Call)
	case msg.CallAnswer != nil:
		log.Printf("Received call_answer for %s", msg.CallAnswer.CallID)
		srv.handleCallAnswer(client, msg.CallAnswer)
	case msg.CallHangup != nil:
		log.Printf("Received call_hangup for %s", msg.CallHangup.CallID)
		srv.handleCallHangup(client, msg.CallHangup)
	case msg.CallDecline != nil:
		log.Printf("Received call_decline for %s", msg.CallDecline.CallID)
		srv.handleCallDecline(client, msg.CallDecline)
	case msg.ICECandidate != nil:
		srv.handleICECandidate(client, msg.ICECandidate)
	case msg.Ping != nil:
		srv.sendMessage(client, &ServerMessage{Pong: &Pong{}})
	case msg.VoicemailList != nil:
		srv.handleVoicemailList(client)
	case msg.VoicemailGet != nil:
		srv.handleVoicemailGet(client, msg.VoicemailGet)
	case msg.VoicemailDelete != nil:
		srv.handleVoicemailDelete(client, msg.VoicemailDelete)
	case msg.VoicemailMarkRead != nil:
		srv.handleVoicemailMarkRead(client, msg.VoicemailMarkRead)
	case msg.DMSend != nil:
		srv.handleDMSend(client, msg.DMSend)
	case msg.DMAck != nil:
		srv.handleDMAck(client, msg.DMAck)
	case msg.DMAttachmentGet != nil:
		srv.handleDMAttachmentGet(client, msg.DMAttachmentGet)
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
		// Unknown identity: a line may only be created via an admin-issued
		// provisioning token. No open self-registration.
		num, err := srv.store.ProvisionUser(
			store.HashToken(msg.ActivationToken),
			msg.KsimID.PublicKey,
			srv.area(),
			displayName,
		)
		if err != nil {
			prefix := ""
			if len(msg.ActivationToken) >= 8 {
				prefix = msg.ActivationToken[:8]
			}
			log.Printf("Provision failed (pubkey=%x token_len=%d token_prefix=%q): %v",
				msg.KsimID.PublicKey, len(msg.ActivationToken), prefix, err)
			switch {
			case err == store.ErrTokenRequired:
				srv.sendError(client, 400, "missing provisioning token in register")
			case err == store.ErrTokenNotFound:
				srv.sendError(client, 403, "invalid provisioning token - generate a fresh QR code from the admin panel")
			case err == store.ErrTokenExpired:
				srv.sendError(client, 403, "provisioning token has expired - generate a new one")
			case err == store.ErrTokenUsed:
				srv.sendError(client, 403, "provisioning token was already used on another phone")
			case err == store.ErrTokenRevoked:
				srv.sendError(client, 403, "provisioning token was revoked by the admin")
			case err == store.ErrNumberTaken:
				srv.sendError(client, 409, "that number was just taken - generate a new provisioning code")
			default:
				srv.sendError(client, 500, "failed to register user")
			}
			return
		}

		user = &store.User{
			KSimPublicKey: msg.KsimID.PublicKey,
			AreaCode:      srv.area(),
			Number:        num,
			DisplayName:   displayName,
			RegisteredAt:  time.Now(),
			LastSeen:      time.Now(),
		}
	} else {
		if displayName != "" {
			srv.store.UpdateDisplayName(msg.KsimID.PublicKey, displayName)
			user.DisplayName = displayName
		}
		srv.store.UpdateLastSeen(msg.KsimID.PublicKey)
	}

	srv.mu.Lock()
	// A reconnect replaces the old socket for the same number. If a previous
	// connection is still alive (e.g. the network dropped but the server has
	// not noticed yet), kick it so it can't linger or clobber this one.
	if prev, ok := srv.clients[user.Number]; ok && prev != client {
		go prev.conn.Close()
	}
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
			IceServers: srv.clientIceServers(),
		},
	})

	log.Printf("User registered: %s (%s)", numbers.FormatNumber(user.AreaCode, user.Number), user.DisplayName)

	// Deliver any call that arrived while this user was offline (FCM wake-up)
	srv.deliverPendingCall(client)
	// Deliver any messages queued while offline (kept until the device acks).
	srv.flushPendingDM(client)
}

func (srv *Server) handleRegisterFCM(client *Client, msg *RegisterFCM) {
	if client.user == nil {
		return
	}
	log.Printf("FCM token registered for %s", client.user.Number)
	if err := srv.store.UpdateFCMToken(client.user.KSimPublicKey, msg.Token); err != nil {
		log.Printf("Failed to update FCM token: %v", err)
	}
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

	areaCode, localNum, err := numbers.ParseNumber(msg.Destination, srv.area())
	if err != nil {
		log.Printf("Call failed: invalid destination: %v", err)
		srv.sendError(client, 400, "invalid destination: "+err.Error())
		return
	}

	log.Printf("Parsed: areaCode=%q localNum=%q (server areaCode=%q)", areaCode, localNum, srv.area())

	// If we have an area code and it doesn't match, it's a cross-server call
	if srv.area() != "" && areaCode != srv.area() {
		reg := srv.registry()
		if reg == nil {
			srv.sendError(client, 501, "cross-server calls require registry federation")
			return
		}
		// 800/900 numbers resolve as network services to a hosting server.
		if areaCode == "800" || areaCode == "900" {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			addr, err := reg.ResolveService(ctx, areaCode+localNum)
			if err != nil {
				srv.sendError(client, 404, "service unavailable")
				return
			}
			srv.startServiceCall(client, addr, areaCode+localNum, msg.Offer)
			return
		}
		srv.startCrossCall(client, reg, areaCode, localNum, msg.Offer)
		return
	}

	srv.mu.RLock()
	calleeClient, online := srv.clients[localNum]
	srv.mu.RUnlock()

	log.Printf("Callee %s online: %v", localNum, online)

	if !online {
		callee, err := srv.store.GetUserByNumber(localNum)
		if err != nil {
			log.Printf("GetUserByNumber error: %v", err)
			srv.sendError(client, 500, "database error")
			return
		}
		if callee == nil {
			srv.sendError(client, 404, "user not online")
			return
		}

		pusher := srv.pushSender()
		if pusher == nil || callee.FCMToken == "" {
			// Unreachable line: offer voicemail when enabled.
			if srv.routeToVoicemail(client, callee, msg.Offer, "") {
				return
			}
			srv.sendError(client, 404, "user not online")
			return
		}

		callID := generateCallID()
		callerName := client.user.DisplayName
		if callerName == "" {
			callerName = client.user.Number
		}

		// Queue the call so it is delivered when the callee reconnects.
		srv.mu.Lock()
		srv.pendingCalls[localNum] = &pendingCall{
			callID:    callID,
			caller:    client,
			offer:     msg.Offer,
			createdAt: time.Now(),
		}
		srv.mu.Unlock()
		client.mu.Lock()
		client.callID = callID
		client.mu.Unlock()

		if err := pusher.SendCallNotification(
			callee.FCMToken, callID, client.user.Number, callerName,
		); err != nil {
			log.Printf("FCM push failed: %v", err)
			srv.removePendingByCallID(callID)
			srv.clearCallID(client, callID)
			if srv.routeToVoicemail(client, callee, msg.Offer, "") {
				return
			}
			srv.sendError(client, 404, "user not online")
			return
		}
		log.Printf("FCM push sent for call %s to %s", callID, localNum)
		// Tell caller we're trying
		srv.sendMessage(client, &ServerMessage{
			CallRinging: &CallRinging{CallID: callID},
		})
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

	srv.startRing(callID, client, calleeClient, msg.Offer)

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

	// Some services (e.g. *02) announce the caller's own number.
	if ca, ok := svc.(services.CallerAware); ok {
		ca.SetCaller(callID, numbers.FormatNumber(client.user.AreaCode, client.user.Number))
	}

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

	// A ring timer may be pending toward this callee — the answer cancels it.
	srv.takeRing(msg.CallID)

	// Cross-server inbound: forward the answer to the remote caller.
	srv.fedMu.Lock()
	leg := srv.inLegs[msg.CallID]
	srv.fedMu.Unlock()
	if leg != nil {
		if leg.callee == nil {
			leg.callee = client
		}
		if err := leg.snd(&ocnserverpb.CallEvent{
			CallId: msg.CallID,
			Type:   ocnserverpb.CallEvent_ANSWER,
			Sdp:    sdpToCommon(msg.Answer),
		}); err != nil {
			log.Printf("Forward answer across bridge: %v", err)
		}
		log.Printf("Bridge answer forwarded for %s", msg.CallID)
		return
	}

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

	// A ring toward an online callee: the caller hanging up cancels it; the
	// callee hanging up while still ringing (legacy clients) counts as a
	// decline and routes the caller into voicemail.
	if rs := srv.takeRing(callID); rs != nil {
		if rs.callee == client {
			if srv.routeToVoicemail(rs.caller, rs.calleeUser, rs.offer, "") {
				log.Printf("Call %s declined; caller routed to voicemail", callID)
				return
			}
			srv.endForClient(rs.caller, callID, "declined")
			return
		}
		srv.clearCallID(rs.callee, callID)
		srv.sendMessage(rs.callee, &ServerMessage{CallEnded: &CallEnded{CallID: callID, Reason: "hangup"}})
		return
	}

	// Cross-server leg: tell the remote side we hung up.
	if srv.closeLocalLeg(client, callID, "hangup") {
		log.Printf("Cross call %s hung up by %s", callID, client.user.Number)
		return
	}

	// If this call is still pending (callee never answered the FCM wake-up),
	// just remove it — the caller is the one hanging up.
	if srv.removePendingByCallID(callID) {
		log.Printf("Pending call %s cancelled by caller %s", callID, client.user.Number)
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

	// Cross-server ICE: relay between the local WS client and the remote leg.
	srv.fedMu.Lock()
	oLeg, oIsOut := srv.outLegs[msg.CallID]
	iLeg, iIsIn := srv.inLegs[msg.CallID]
	srv.fedMu.Unlock()
	if oIsOut {
		// Local caller's candidate -> remote callee.
		if err := oLeg.snd(&ocnserverpb.CallEvent{
			CallId:    msg.CallID,
			Type:      ocnserverpb.CallEvent_ICE,
			Candidate: iceToCommon(msg.Candidate),
		}); err != nil {
			log.Printf("Bridge ICE send failed: %v", err)
		}
		return
	}
	if iIsIn && iLeg.callee == client {
		// Local callee's candidate -> remote caller.
		if err := iLeg.snd(&ocnserverpb.CallEvent{
			CallId:    msg.CallID,
			Type:      ocnserverpb.CallEvent_ICE,
			Candidate: iceToCommon(msg.Candidate),
		}); err != nil {
			log.Printf("Bridge ICE send failed: %v", err)
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

func (srv *Server) handleCallDecline(client *Client, msg *CallDecline) {
	if client.user == nil {
		return
	}
	log.Printf("Decline from %s for call %s", client.user.Number, msg.CallID)

	client.mu.Lock()
	callID := client.callID
	if msg.CallID != "" {
		callID = msg.CallID
	}
	client.mu.Unlock()
	if callID == "" {
		return
	}

	rs := srv.takeRing(callID)
	if rs == nil || rs.callee != client {
		// Not a tracked ring — behave like a hangup.
		srv.handleCallHangup(client, &CallHangup{CallID: msg.CallID})
		return
	}
	srv.clearCallID(client, callID)
	if srv.routeToVoicemail(rs.caller, rs.calleeUser, rs.offer, "") {
		log.Printf("Call %s declined; caller routed to voicemail", callID)
		return
	}
	srv.endForClient(rs.caller, callID, "declined")
}

// ---- Ring management ----

func (srv *Server) startRing(callID string, caller, callee *Client, offer *SDPSession) {
	rs := &ringState{caller: caller, callee: callee, calleeUser: callee.user, offer: offer}
	srv.ringMu.Lock()
	rs.timer = time.AfterFunc(ringTimeout, func() { srv.onRingTimeout(callID) })
	srv.ringStates[callID] = rs
	srv.ringMu.Unlock()
}

// takeRing removes and stops any ring state for callID.
func (srv *Server) takeRing(callID string) *ringState {
	srv.ringMu.Lock()
	defer srv.ringMu.Unlock()
	if rs := srv.ringStates[callID]; rs != nil {
		if rs.timer != nil {
			rs.timer.Stop()
		}
		delete(srv.ringStates, callID)
		return rs
	}
	return nil
}

func (srv *Server) onRingTimeout(callID string) {
	srv.ringMu.Lock()
	rs := srv.ringStates[callID]
	if rs == nil {
		srv.ringMu.Unlock()
		return
	}
	delete(srv.ringStates, callID)
	srv.ringMu.Unlock()

	if rs.answered {
		return
	}
	log.Printf("Call %s to %s rang out", callID, rs.calleeUser.Number)

	// Stop the callee's ring.
	srv.clearCallID(rs.callee, callID)
	srv.sendMessage(rs.callee, &ServerMessage{CallEnded: &CallEnded{CallID: callID, Reason: "no answer"}})

	if !srv.isOnline(rs.caller) {
		return
	}
	if srv.routeToVoicemail(rs.caller, rs.calleeUser, rs.offer, "") {
		return
	}
	srv.endForClient(rs.caller, callID, "no answer")
}

func (srv *Server) clearCallID(client *Client, callID string) {
	if client == nil {
		return
	}
	client.mu.Lock()
	if client.callID == callID {
		client.callID = ""
	}
	client.mu.Unlock()
}

func (srv *Server) endForClient(client *Client, callID, reason string) {
	if client == nil {
		return
	}
	srv.clearCallID(client, callID)
	srv.sendMessage(client, &ServerMessage{CallEnded: &CallEnded{CallID: callID, Reason: reason}})
}

// ---- Voicemail ----

// routeToVoicemail answers caller with a recording session addressed to
// recipient. Returns false when voicemail isn't available (disabled, recipient
// unknown, or the call setup failed), in which case the caller should be told
// the call could not be completed normally.
func (srv *Server) routeToVoicemail(caller *Client, recipient *store.User, offer *SDPSession, reuseCallID string) bool {
	vm := srv.voicemailManager()
	if vm == nil || !vm.Enabled() || recipient == nil || offer == nil || !srv.isOnline(caller) {
		return false
	}

	callID := reuseCallID
	if callID == "" {
		callID = generateCallID()
	}

	lv := voicemail.NewLeaveService(
		vm,
		recipient,
		canonicalNumber(caller.user.AreaCode, caller.user.Number),
		caller.user.DisplayName,
	)
	lv.OnSelfEnd = func(id string) {
		srv.endServiceCall(caller, id, "voicemail complete")
	}

	webrtcOffer := &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer.SDP}
	answer, err := lv.HandleCall(callID, webrtcOffer, srv.makeSendICE(caller, callID))
	if err != nil {
		log.Printf("voicemail: failed to answer caller %s: %v", caller.user.Number, err)
		return false
	}

	srv.mu.Lock()
	srv.svcCalls[callID] = &serviceCall{client: caller, service: lv}
	caller.callID = callID
	srv.mu.Unlock()

	srv.sendMessage(caller, &ServerMessage{
		CallConnected: &CallConnected{
			CallID: callID,
			Answer: &SDPSession{SDP: answer.SDP, Type: "answer"},
			Service: &ServiceInfo{
				Code: "vm",
				Name: "Voicemail",
			},
		},
	})
	log.Printf("voicemail: caller %s recording for %s (call %s)", caller.user.Number, recipient.Number, callID)
	return true
}

// makeSendICE returns a closure that relays a service's ICE candidate to
// client for callID (used by voicemail + service calls).
func (srv *Server) makeSendICE(client *Client, callID string) func(webrtc.ICECandidateInit) {
	return func(candidate webrtc.ICECandidateInit) {
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
}

// endServiceCall tears down a service call for a client and notifies them.
func (srv *Server) endServiceCall(caller *Client, callID, reason string) {
	srv.mu.Lock()
	sc, ok := srv.svcCalls[callID]
	if ok {
		delete(srv.svcCalls, callID)
	}
	srv.mu.Unlock()
	if caller != nil {
		srv.clearCallID(caller, callID)
		srv.sendMessage(caller, &ServerMessage{CallEnded: &CallEnded{CallID: callID, Reason: reason}})
	}
	if ok && sc != nil && sc.service != nil {
		_ = sc.service.EndCall(callID)
	}
}

func (srv *Server) handleVoicemailList(client *Client) {
	if client.user == nil {
		return
	}
	meta, err := srv.store.ListVoicemailMeta(client.user.Number)
	if err != nil {
		log.Printf("voicemail list error: %v", err)
		srv.sendError(client, 500, "failed to list voicemail")
		return
	}
	unread, _ := srv.store.CountUnlistened(client.user.Number)
	msgs := make([]VoicemailInfo, 0, len(meta))
	for _, m := range meta {
		msgs = append(msgs, VoicemailInfo{
			ID:              m.ID,
			CallerNumber:    m.CallerNumber,
			CallerName:      m.CallerName,
			DurationSeconds: m.DurationSeconds,
			Listened:        m.Listened,
			CreatedAt:       m.CreatedAt.Unix(),
		})
	}
	srv.sendMessage(client, &ServerMessage{
		VoicemailListResponse: &VoicemailListResponse{Messages: msgs, Unread: unread},
	})
}

func (srv *Server) handleVoicemailGet(client *Client, req *VoicemailGetReq) {
	if client.user == nil || req == nil {
		return
	}
	vm := srv.voicemailManager()
	if vm == nil || !vm.Enabled() {
		srv.sendError(client, 503, "voicemail unavailable")
		return
	}
	msg, err := srv.store.GetVoicemail(req.ID, client.user.Number)
	if err != nil || msg == nil {
		srv.sendError(client, 404, "voicemail not found")
		return
	}
	ogg, err := vm.RenderOgg(client.user.Number, msg)
	if err != nil {
		log.Printf("voicemail render error: %v", err)
		srv.sendError(client, 500, "failed to load voicemail")
		return
	}
	srv.sendMessage(client, &ServerMessage{
		VoicemailGetResponse: &VoicemailGetResponse{
			ID:          msg.ID,
			AudioBase64: base64.StdEncoding.EncodeToString(ogg),
		},
	})
}

func (srv *Server) handleVoicemailDelete(client *Client, req *VoicemailDeleteReq) {
	if client.user == nil || req == nil {
		return
	}
	if err := srv.store.DeleteVoicemail(req.ID, client.user.Number); err != nil {
		log.Printf("voicemail delete error: %v", err)
	}
}

func (srv *Server) handleVoicemailMarkRead(client *Client, req *VoicemailMarkReadReq) {
	if client.user == nil || req == nil {
		return
	}
	if err := srv.store.MarkListened(req.ID, client.user.Number); err != nil {
		log.Printf("voicemail mark read error: %v", err)
	}
}

// ---- Direct messaging ----

func (srv *Server) handleDMSend(client *Client, req *DMSend) {
	if client.user == nil {
		srv.sendError(client, 401, "not registered")
		return
	}
	if req == nil || req.To == "" {
		srv.sendError(client, 400, "missing destination")
		return
	}
	if req.Kind != "text" && req.Kind != "image" {
		srv.sendError(client, 400, "invalid message kind")
		return
	}
	if req.Kind == "text" && strings.TrimSpace(req.Text) == "" {
		srv.sendError(client, 400, "empty message")
		return
	}
	if req.Kind == "image" && (req.Image == nil || req.Image.B64 == "") {
		srv.sendError(client, 400, "missing image data")
		return
	}
	if req.Kind == "image" && len(req.Image.B64) > (dm.MaxImageBytes*4/3)+8 {
		srv.sendError(client, 413, "image too large")
		return
	}

	area, local, err := numbers.ParseNumber(req.To, srv.area())
	if err != nil {
		srv.sendError(client, 400, "invalid destination: "+err.Error())
		return
	}
	mgr := srv.dmManager()
	if mgr == nil {
		srv.sendError(client, 503, "messaging unavailable")
		return
	}

	env := &dm.Envelope{
		MessageID: uuid.NewString(),
		ClientID:  req.ClientID,
		From:      dm.Canonical(client.user.AreaCode, client.user.Number),
		FromName:  client.user.DisplayName,
		To:        dm.Canonical(area, local),
		Kind:      req.Kind,
		Text:      req.Text,
		CreatedAt: time.Now().UnixMilli(),
	}
	if req.Image != nil {
		env.Image = &dm.Image{Name: req.Image.Name, Mime: req.Image.Mime, B64: req.Image.B64}
	}

	// Remote recipient: relay to their home server over the federation link.
	if srv.area() != "" && area != srv.area() {
		if !srv.sendRemoteDM(client, env, area) {
			return
		}
		srv.sendMessage(client, &ServerMessage{DMEvent: &DMEvent{
			Type: "status", MessageID: env.MessageID, ClientID: req.ClientID, Status: "delivered",
		}})
		return
	}

	callee, err := srv.store.GetUserByNumber(local)
	if err != nil {
		log.Printf("dm_send: lookup error: %v", err)
		srv.sendError(client, 500, "database error")
		return
	}
	if callee == nil {
		srv.sendError(client, 404, "user not found")
		return
	}

	// Authoritative outbox copy; dropped when the device acks.
	if _, err := mgr.Enqueue(local, env); err != nil {
		log.Printf("dm_send: enqueue failed: %v", err)
		srv.sendError(client, 500, "failed to send")
		return
	}

	if srv.relayDMMessage(local, env) {
		log.Printf("dm_send: relayed %s to online %s", env.MessageID, local)
	} else if callee.FCMToken != "" {
		p := srv.pushSender()
		if p != nil {
			if err := p.SendMessageNotification(callee.FCMToken, env.From, env.FromName); err != nil {
				log.Printf("dm_send: push failed: %v", err)
			}
		}
	}

	// Acknowledge delivery to the network.
	srv.sendMessage(client, &ServerMessage{DMEvent: &DMEvent{
		Type: "status", MessageID: env.MessageID, ClientID: req.ClientID, Status: "delivered",
	}})
}

// sendRemoteDM relays an envelope to the home server for a remote area.
// Returns true when accepted.
func (srv *Server) sendRemoteDM(client *Client, env *dm.Envelope, area string) bool {
	reg := srv.registry()
	if reg == nil {
		srv.sendError(client, 501, "cross-server messaging requires federation")
		return false
	}
	routeCtx, rcancel := context.WithTimeout(context.Background(), 15*time.Second)
	addr, err := reg.Route(routeCtx, area)
	rcancel()
	if err != nil {
		log.Printf("dm_send: no route for area %s: %v", area, err)
		srv.sendError(client, 404, "number unreachable (no route)")
		return false
	}
	conn, err := srv.bridgeConn(addr, srv.fedInsecure())
	if err != nil {
		log.Printf("dm_send: dial %s: %v", addr, err)
		srv.sendError(client, 502, "delivery failed")
		return false
	}
	cli := ocnserverpb.NewOCNServerServiceClient(conn)
	dmCtx, dcancel := context.WithTimeout(context.Background(), 20*time.Second)
	resp, err := cli.DeliverDM(dmCtx, dmEnvelopeToProto(env))
	dcancel()
	if err != nil {
		log.Printf("dm_send: DeliverDM to %s failed: %v", addr, err)
		srv.sendError(client, 502, "delivery failed")
		return false
	}
	if !resp.GetAccepted() {
		log.Printf("dm_send: remote rejected: %s", resp.GetErrorMessage())
		srv.sendError(client, 404, "delivery failed: "+resp.GetErrorMessage())
		return false
	}
	log.Printf("dm_send: relayed %s to area %s via %s", env.MessageID, area, addr)
	return true
}

func (srv *Server) handleDMAck(client *Client, req *DMAck) {
	if client.user == nil || req == nil || req.MessageID == "" {
		return
	}
	if mgr := srv.dmManager(); mgr != nil {
		if err := mgr.Remove(req.MessageID, client.user.Number); err != nil {
			log.Printf("dm_ack: remove failed: %v", err)
		}
	}
}

func (srv *Server) handleDMAttachmentGet(client *Client, req *DMAttachmentGet) {
	if client.user == nil || req == nil {
		return
	}
	mgr := srv.dmManager()
	if mgr == nil {
		srv.sendError(client, 503, "messaging unavailable")
		return
	}
	env, err := mgr.Get(req.MessageID, client.user.Number)
	if err != nil || env == nil || env.Image == nil {
		srv.sendError(client, 404, "attachment not found")
		return
	}
	srv.sendMessage(client, &ServerMessage{DMAttachment: &DMAttachment{
		MessageID: req.MessageID,
		Name:      env.Image.Name,
		Mime:      env.Image.Mime,
		B64:       env.Image.B64,
	}})
}

// relayDMMessage pushes an inbound envelope to an online recipient as a
// "new" event (image bytes are fetched separately). Returns whether it was
// relayed.
func (srv *Server) relayDMMessage(recipientLocal string, env *dm.Envelope) bool {
	srv.mu.RLock()
	c, ok := srv.clients[recipientLocal]
	srv.mu.RUnlock()
	if !ok || c == nil {
		return false
	}
	ev := &DMEvent{
		Type: "new", MessageID: env.MessageID,
		From: env.From, FromName: env.FromName,
		Kind: env.Kind, Text: env.Text, CreatedAt: env.CreatedAt,
	}
	if env.Image != nil {
		ev.ImageName = env.Image.Name
		ev.ImageMime = env.Image.Mime
	}
	srv.sendMessage(c, &ServerMessage{DMEvent: ev})
	return true
}

// flushPendingDM delivers any queued messages to a freshly-registered client
// (they stay queued until the device acks, so a lost frame re-delivers later).
func (srv *Server) flushPendingDM(client *Client) {
	mgr := srv.dmManager()
	if mgr == nil || client == nil || client.user == nil {
		return
	}
	envs, err := mgr.Pending(client.user.Number)
	if err != nil {
		log.Printf("dm flush error for %s: %v", client.user.Number, err)
		return
	}
	for _, env := range envs {
		srv.relayDMMessage(client.user.Number, env)
	}
	if len(envs) > 0 {
		log.Printf("dm: flushed %d queued message(s) to %s", len(envs), client.user.Number)
	}
}

// NotifyVoicemailStored tells a recipient about a new message: a WS event when
// they are online, otherwise an FCM push. Used as the voicemail Manager hook.
func (srv *Server) NotifyVoicemailStored(recipientNumber, callerNumber, callerName string) {
	unread, _ := srv.store.CountUnlistened(recipientNumber)
	srv.mu.RLock()
	c, online := srv.clients[recipientNumber]
	srv.mu.RUnlock()
	if online && c != nil {
		srv.sendMessage(c, &ServerMessage{
			VoicemailEvent: &VoicemailEvent{
				Action:       "new",
				CallerNumber: callerNumber,
				CallerName:   callerName,
				Unread:       unread,
			},
		})
		return
	}
	u, err := srv.store.GetUserByNumber(recipientNumber)
	if err != nil || u == nil || u.FCMToken == "" {
		return
	}
	p := srv.pushSender()
	if p == nil {
		return
	}
	if err := p.SendVoicemailNotification(u.FCMToken, callerNumber, callerName); err != nil {
		log.Printf("voicemail push failed for %s: %v", recipientNumber, err)
	}
}

func canonicalNumber(area, number string) string {
	if area == "" {
		return number
	}
	return area + number
}

func (srv *Server) isOnline(client *Client) bool {
	if client == nil || client.user == nil {
		return false
	}
	srv.mu.RLock()
	defer srv.mu.RUnlock()
	c, ok := srv.clients[client.user.Number]
	return ok && c == client
}

// deliverPendingCall delivers a queued call to a callee who just reconnected
// after being woken by an FCM push.
func (srv *Server) deliverPendingCall(callee *Client) {
	if callee == nil || callee.user == nil {
		return
	}

	srv.mu.Lock()
	p, ok := srv.pendingCalls[callee.user.Number]
	if ok {
		delete(srv.pendingCalls, callee.user.Number)
	}
	srv.mu.Unlock()
	if !ok {
		return
	}

	var callerArea, callerNum, callerName string
	remote := p.remote
	if remote {
		// Caller is on a remote server; caller info lives on the bridge leg.
		srv.fedMu.Lock()
		leg := srv.inLegs[p.callID]
		if leg != nil {
			leg.callee = callee
			callerArea = leg.callerArea
			callerNum = leg.callerNum
			callerName = leg.callerName
		}
		srv.fedMu.Unlock()
		if leg == nil {
			log.Printf("Pending cross call %s dropped: remote leg gone", p.callID)
			return
		}
	} else {
		// If the caller gave up or disconnected while waiting, don't ring.
		if !srv.isOnline(p.caller) {
			log.Printf("Pending call %s dropped: caller offline", p.callID)
			return
		}
		callerArea = p.caller.user.AreaCode
		callerNum = p.caller.user.Number
		callerName = p.caller.user.DisplayName
	}

	log.Printf("Delivering pending call %s to %s", p.callID, callee.user.Number)

	srv.sendMessage(callee, &ServerMessage{
		IncomingCall: &IncomingCall{
			CallID: p.callID,
			CallerNumber: &PhoneNumber{
				AreaCode: callerArea,
				Number:   callerNum,
			},
			CallerName: &DisplayName{Name: callerName},
			Offer:      p.offer,
		},
	})

	if !remote {
		p.caller.mu.Lock()
		p.caller.callID = p.callID
		p.caller.mu.Unlock()
	}
	callee.mu.Lock()
	callee.callID = p.callID
	callee.mu.Unlock()
}

func (srv *Server) removePendingByCallID(callID string) bool {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	for num, p := range srv.pendingCalls {
		if p.callID == callID {
			delete(srv.pendingCalls, num)
			return true
		}
	}
	return false
}

// closeLocalLeg notifies the remote side when a local WS client (caller or
// callee) is ending a cross-server call. Returns true when handled.
func (srv *Server) closeLocalLeg(client *Client, callID, reason string) bool {
	srv.fedMu.Lock()
	defer srv.fedMu.Unlock()
	if leg, ok := srv.outLegs[callID]; ok {
		delete(srv.outLegs, callID)
		_ = leg.snd(&ocnserverpb.CallEvent{CallId: callID, Type: ocnserverpb.CallEvent_HANGUP, Reason: reason})
		return true
	}
	if leg, ok := srv.inLegs[callID]; ok && leg.callee == client {
		delete(srv.inLegs, callID)
		_ = leg.snd(&ocnserverpb.CallEvent{CallId: callID, Type: ocnserverpb.CallEvent_HANGUP, Reason: reason})
		return true
	}
	return false
}

func (srv *Server) expirePendingCalls() {
	type expiredCall struct {
		num string
		p   *pendingCall
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		var expired []expiredCall
		srv.mu.Lock()
		for num, p := range srv.pendingCalls {
			if time.Since(p.createdAt) > pendingCallTimeout {
				expired = append(expired, expiredCall{num: num, p: p})
				delete(srv.pendingCalls, num)
			}
		}
		srv.mu.Unlock()

		for _, ec := range expired {
			p := ec.p
			log.Printf("Pending call %s to %s expired (no answer)", p.callID, ec.num)
			if p.remote {
				// Notify the remote caller and drop the leg.
				srv.fedMu.Lock()
				leg := srv.inLegs[p.callID]
				delete(srv.inLegs, p.callID)
				srv.fedMu.Unlock()
				if leg != nil {
					_ = leg.snd(&ocnserverpb.CallEvent{
						CallId: p.callID,
						Type:   ocnserverpb.CallEvent_HANGUP,
						Reason: "no answer",
					})
				}
				continue
			}
			if srv.isOnline(p.caller) {
				recipient, _ := srv.store.GetUserByNumber(ec.num)
				if srv.routeToVoicemail(p.caller, recipient, p.offer, "") {
					continue
				}
				srv.clearCallID(p.caller, p.callID)
				srv.sendMessage(p.caller, &ServerMessage{
					CallEnded: &CallEnded{CallID: p.callID, Reason: "no answer"},
				})
			}
		}
	}
}

// OnlineNumbers returns the set of phone numbers with a live WebSocket
// connection (7-digit local numbers). Used by the admin panel.
func (srv *Server) OnlineNumbers() map[string]bool {
	srv.mu.RLock()
	defer srv.mu.RUnlock()
	out := make(map[string]bool, len(srv.clients))
	for num := range srv.clients {
		out[num] = true
	}
	return out
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
		// Notify a remote leg if this client was in a cross-server call.
		srv.closeLocalLeg(client, callID, "disconnect")

		// If the caller disconnects while the call is still pending, drop it.
		srv.removePendingByCallID(callID)

		// Ring toward an online callee: a disconnecting callee lets the caller
		// reach voicemail; a disconnecting caller cancels the ring.
		if rs := srv.takeRing(callID); rs != nil {
			if rs.callee == client {
				if srv.isOnline(rs.caller) {
					if !srv.routeToVoicemail(rs.caller, rs.calleeUser, rs.offer, "") {
						srv.endForClient(rs.caller, callID, "disconnect")
					}
				}
			} else if rs.caller == client {
				srv.clearCallID(rs.callee, callID)
				srv.sendMessage(rs.callee, &ServerMessage{CallEnded: &CallEnded{CallID: callID, Reason: "disconnect"}})
			}
		}

		// Tear down any service session this client was in (e.g. voicemail).
		srv.mu.Lock()
		sc, isSvc := srv.svcCalls[callID]
		if isSvc && sc.client == client {
			delete(srv.svcCalls, callID)
		}
		srv.mu.Unlock()
		if isSvc && sc.client == client && sc.service != nil {
			_ = sc.service.EndCall(callID)
		}

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
	// Only remove this number if it still points at this client. A stale
	// socket from a superseded reconnect must not evict the live connection.
	if cur, ok := srv.clients[client.user.Number]; ok && cur == client {
		delete(srv.clients, client.user.Number)
	}
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
	var b [4]byte
	if _, err := rand.Read(b[:]); err == nil {
		return fmt.Sprintf("%x%x", time.Now().UnixNano(), b)
	}
	return fmt.Sprintf("%x", time.Now().UnixNano())
}
