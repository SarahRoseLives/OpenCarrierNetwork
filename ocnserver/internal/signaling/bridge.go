package signaling

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/open-carrier-network/ocn/internal/services"
	commonpb "github.com/open-carrier-network/ocn/proto/common"
	ocnserverpb "github.com/open-carrier-network/ocn/proto/ocnserver"
	"github.com/pion/webrtc/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// outLeg is an outbound cross-server leg: this server hosts the CALLER and the
// callee lives on a remote server reached over the given bridge stream.
type outLeg struct {
	snd func(ev *ocnserverpb.CallEvent) error
}

// inLeg is an inbound cross-server leg: this server hosts the CALLEE and the
// caller is on a remote server. callee is nil until the local user is online;
// service is set when this is a hosted 800/900 network service.
type inLeg struct {
	snd        func(ev *ocnserverpb.CallEvent) error
	callee     *Client
	calleeNum  string
	callerArea string
	callerNum  string
	callerName string
	service    services.Service
}

// eventSender adapts a gRPC bridge stream's Send for the leg maps.
func eventSender(send func(*ocnserverpb.CallEvent) error) func(*ocnserverpb.CallEvent) error {
	return send
}

// ---- connection caching for outgoing inter-server calls ----

func (srv *Server) bridgeConn(addr string, insecureConn bool) (*grpc.ClientConn, error) {
	srv.fedMu.Lock()
	defer srv.fedMu.Unlock()
	if c, ok := srv.gConns[addr]; ok {
		return c, nil
	}
	opts := []grpc.DialOption{grpc.WithUserAgent("ocn-server/1")}
	if insecureConn {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(nil)))
	}
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, err
	}
	srv.gConns[addr] = conn
	return conn, nil
}

// startCrossCall begins a call to a number on a remote server (identified by
// its area code). Mirrors the local handleCall online path: the caller gets a
// call_ringing once the remote side confirms, then call_connected/answer, ICE,
// and hangup events flow over the bridge.
func (srv *Server) startCrossCall(client *Client, reg registryClient, remoteArea, localNum string, offer *SDPSession) {
	if client.user == nil {
		srv.sendError(client, 401, "not registered")
		return
	}

	routeCtx, routeCancel := context.WithTimeout(context.Background(), 15*time.Second)
	addr, err := reg.Route(routeCtx, remoteArea)
	routeCancel()
	if err != nil {
		log.Printf("Cross call: no route for area %s: %v", remoteArea, err)
		srv.sendError(client, 404, "number unreachable (no route)")
		return
	}

	conn, err := srv.bridgeConn(addr, srv.fedInsecure())
	if err != nil {
		log.Printf("Cross call: dial %s: %v", addr, err)
		srv.sendError(client, 502, "destination server unreachable")
		return
	}
	svc := ocnserverpb.NewOCNServerServiceClient(conn)
	// The stream lives for the whole call; no deadline here.
	stream, err := svc.BridgeCall(context.Background())
	if err != nil {
		log.Printf("Cross call: bridge to %s: %v", addr, err)
		srv.sendError(client, 502, "destination server unreachable")
		return
	}

	callID := generateCallID()
	callerName := client.user.DisplayName
	if callerName == "" {
		callerName = client.user.Number
	}

	if err := stream.Send(&ocnserverpb.CallEvent{
		CallId: callID,
		Type:   ocnserverpb.CallEvent_INCOMING,
		CallerNumber: &commonpb.PhoneNumber{
			AreaCode: srv.area(),
			Number:   client.user.Number,
		},
		CallerName:  &commonpb.DisplayName{Name: callerName},
		Destination: localNum,
		Sdp:         sdpToCommon(offer),
	}); err != nil {
		log.Printf("Cross call: send incoming: %v", err)
		srv.sendError(client, 502, "destination server unreachable")
		return
	}

	client.mu.Lock()
	client.callID = callID
	client.mu.Unlock()

	srv.fedMu.Lock()
	srv.outLegs[callID] = &outLeg{snd: stream.Send}
	srv.fedMu.Unlock()

	log.Printf("Cross call %s: caller %s%s -> area %s num %s via %s",
		callID, srv.area(), client.user.Number, remoteArea, localNum, addr)

	go srv.outboundReadLoop(stream, callID, client)
}

// startServiceCall bridges a call to a full 800/900 service number hosted on
// the given remote server (resolved via the registry).
func (srv *Server) startServiceCall(client *Client, addr, fullNumber string, offer *SDPSession) {
	if client.user == nil {
		srv.sendError(client, 401, "not registered")
		return
	}
	conn, err := srv.bridgeConn(addr, srv.fedInsecure())
	if err != nil {
		srv.sendError(client, 502, "service host unreachable")
		return
	}
	svc := ocnserverpb.NewOCNServerServiceClient(conn)
	stream, err := svc.BridgeCall(context.Background())
	if err != nil {
		srv.sendError(client, 502, "service host unreachable")
		return
	}

	callID := generateCallID()
	callerName := client.user.DisplayName
	if callerName == "" {
		callerName = client.user.Number
	}
	if err := stream.Send(&ocnserverpb.CallEvent{
		CallId: callID,
		Type:   ocnserverpb.CallEvent_INCOMING,
		CallerNumber: &commonpb.PhoneNumber{
			AreaCode: srv.area(),
			Number:   client.user.Number,
		},
		CallerName:  &commonpb.DisplayName{Name: callerName},
		Destination: fullNumber,
		Sdp:         sdpToCommon(offer),
	}); err != nil {
		srv.sendError(client, 502, "service host unreachable")
		return
	}

	client.mu.Lock()
	client.callID = callID
	client.mu.Unlock()
	srv.fedMu.Lock()
	srv.outLegs[callID] = &outLeg{snd: stream.Send}
	srv.fedMu.Unlock()
	log.Printf("Service call %s: %s dialed %s via %s", callID, client.user.Number, fullNumber, addr)
	go srv.outboundReadLoop(stream, callID, client)
}

// outboundReadLoop relays events from the remote callee's server to the local
// caller over its WebSocket.
func (srv *Server) outboundReadLoop(stream ocnserverpb.OCNServerService_BridgeCallClient, callID string, caller *Client) {
	defer func() {
		srv.fedMu.Lock()
		delete(srv.outLegs, callID)
		srv.fedMu.Unlock()
		caller.mu.Lock()
		if caller.callID == callID {
			caller.callID = ""
		}
		caller.mu.Unlock()
	}()

	for {
		ev, err := stream.Recv()
		if err != nil {
			if err != io.EOF {
				log.Printf("Cross call %s: bridge closed: %v", callID, err)
				// Remote end disappeared unexpectedly.
				srv.sendMessage(caller, &ServerMessage{
					CallEnded: &CallEnded{CallID: callID, Reason: "disconnect"},
				})
			}
			return
		}

		switch ev.Type {
		case ocnserverpb.CallEvent_RINGING:
			srv.sendMessage(caller, &ServerMessage{CallRinging: &CallRinging{CallID: callID}})
		case ocnserverpb.CallEvent_ANSWER:
			if ev.Sdp != nil {
				srv.sendMessage(caller, &ServerMessage{
					CallConnected: &CallConnected{
						CallID: callID,
						Answer: commonToSdp(ev.Sdp),
					},
				})
			}
		case ocnserverpb.CallEvent_ICE:
			if ev.Candidate != nil {
				srv.sendMessage(caller, &ServerMessage{
					ICECandidate: &ICECandidateTrickle{
						CallID: callID,
						Candidate: &ICECandidate{
							Candidate:     ev.Candidate.Candidate,
							SDPMid:        ev.Candidate.SdpMid,
							SDPMLineIndex: ev.Candidate.SdpMlineIndex,
						},
					},
				})
			}
		case ocnserverpb.CallEvent_HANGUP, ocnserverpb.CallEvent_BUSY:
			reason := ev.Reason
			if reason == "" {
				reason = "hangup"
			}
			srv.sendMessage(caller, &ServerMessage{
				CallEnded: &CallEnded{CallID: callID, Reason: reason},
			})
			return
		}
	}
}

// NewGRPCBridge returns the OCNServerService server (BridgeCall) for this
// signaling server, to be mounted on the server's inter-server gRPC endpoint.
func NewGRPCBridge(srv *Server) ocnserverpb.OCNServerServiceServer {
	return &grpcBridge{srv: srv}
}

type grpcBridge struct {
	ocnserverpb.UnimplementedOCNServerServiceServer
	srv *Server
}

func (g *grpcBridge) BridgeCall(stream ocnserverpb.OCNServerService_BridgeCallServer) error {
	return g.srv.serveInbound(stream)
}

// serveInbound handles an inter-server stream where THIS server hosts the
// callee. It rings the local callee (or queues + pushes when offline) and
// relays answer/ICE/hangup events between the remote caller and the callee.
func (srv *Server) serveInbound(stream ocnserverpb.OCNServerService_BridgeCallServer) error {
	var opened []string
	defer func() {
		srv.fedMu.Lock()
		for _, callID := range opened {
			if leg, ok := srv.inLegs[callID]; ok {
				delete(srv.inLegs, callID)
				if leg.service != nil {
					_ = leg.service.EndCall(callID)
				} else if leg.callee != nil {
					leg.callee.mu.Lock()
					if leg.callee.callID == callID {
						leg.callee.callID = ""
					}
					leg.callee.mu.Unlock()
					srv.sendMessage(leg.callee, &ServerMessage{
						CallEnded: &CallEnded{CallID: callID, Reason: "disconnect"},
					})
				}
			}
			srv.removePendingByCallID(callID)
		}
		srv.fedMu.Unlock()
	}()

	for {
		ev, err := stream.Recv()
		if err != nil {
			if err != io.EOF {
				log.Printf("Bridge inbound closed: %v", err)
			}
			return nil
		}

		switch ev.Type {
		case ocnserverpb.CallEvent_INCOMING:
			if id := srv.inboundIncoming(stream, ev); id != "" {
				opened = append(opened, id)
			}
		case ocnserverpb.CallEvent_ICE:
			srv.fedMu.Lock()
			leg := srv.inLegs[ev.CallId]
			srv.fedMu.Unlock()
			if leg == nil || ev.Candidate == nil {
				continue
			}
			if leg.service != nil {
				if ice, ok := leg.service.(services.CallICE); ok {
					if err := ice.HandleCallICE(ev.CallId, commonToICEInit(ev.Candidate)); err != nil {
						log.Printf("Bridge service ICE error: %v", err)
					}
				} else if err := leg.service.HandleICE(commonToICEInit(ev.Candidate)); err != nil {
					log.Printf("Bridge service ICE error: %v", err)
				}
				continue
			}
			if leg.callee == nil {
				continue
			}
			srv.sendMessage(leg.callee, &ServerMessage{
				ICECandidate: &ICECandidateTrickle{
					CallID: ev.CallId,
					Candidate: &ICECandidate{
						Candidate:     ev.Candidate.Candidate,
						SDPMid:        ev.Candidate.SdpMid,
						SDPMLineIndex: ev.Candidate.SdpMlineIndex,
					},
				},
			})
		case ocnserverpb.CallEvent_HANGUP:
			srv.inboundRemoteHangup(ev.CallId, ev.Reason)
		}
	}
}

// inboundIncoming processes a remote caller's INCOMING event.
func (srv *Server) inboundIncoming(stream ocnserverpb.OCNServerService_BridgeCallServer, ev *ocnserverpb.CallEvent) string {
	snd := eventSender(stream.Send)
	callID := ev.CallId
	if callID == "" {
		callID = generateCallID()
	}
	calleeNum := ev.Destination

	leg := &inLeg{
		snd:        snd,
		calleeNum:  calleeNum,
		callerArea: ev.CallerNumber.GetAreaCode(),
		callerNum:  ev.CallerNumber.GetNumber(),
		callerName: ev.CallerName.GetName(),
	}

	// Hosted 800/900 network service?
	if svc := srv.networkService(calleeNum); svc != nil {
		leg.service = svc
		srv.fedMu.Lock()
		srv.inLegs[callID] = leg
		srv.fedMu.Unlock()
		if !srv.handleNetService(snd, ev, callID, leg, svc) {
			srv.removeInLeg(callID)
			return ""
		}
		return callID
	}

	srv.fedMu.Lock()
	srv.inLegs[callID] = leg
	srv.fedMu.Unlock()

	srv.mu.RLock()
	calleeClient, online := srv.clients[calleeNum]
	srv.mu.RUnlock()

	if online {
		leg.callee = calleeClient
		calleeClient.mu.Lock()
		calleeClient.callID = callID
		calleeClient.mu.Unlock()

		srv.sendMessage(calleeClient, &ServerMessage{
			IncomingCall: &IncomingCall{
				CallID: callID,
				CallerNumber: &PhoneNumber{
					AreaCode: leg.callerArea,
					Number:   leg.callerNum,
				},
				CallerName: &DisplayName{Name: leg.callerName},
				Offer:      commonToSdp(ev.Sdp),
			},
		})
		snd(&ocnserverpb.CallEvent{CallId: callID, Type: ocnserverpb.CallEvent_RINGING})
		log.Printf("Bridge call %s ringing local callee %s (from %s%s)", callID, calleeNum, leg.callerArea, leg.callerNum)
		return callID
	}

	// Callee offline: queue + wake via the configured push sender.
	callee, err := srv.store.GetUserByNumber(calleeNum)
	pusher := srv.pushSender()
	if err != nil || callee == nil || callee.FCMToken == "" || pusher == nil {
		snd(&ocnserverpb.CallEvent{CallId: callID, Type: ocnserverpb.CallEvent_BUSY, Reason: "callee unavailable"})
		srv.removeInLeg(callID)
		return ""
	}

	srv.mu.Lock()
	srv.pendingCalls[calleeNum] = &pendingCall{
		callID:    callID,
		remote:    true,
		offer:     commonToSdp(ev.Sdp),
		createdAt: time.Now(),
	}
	srv.mu.Unlock()

	if err := pusher.SendCallNotification(callee.FCMToken, callID, fmt.Sprintf("%s%s", leg.callerArea, leg.callerNum), leg.callerName); err != nil {
		log.Printf("Bridge call %s: push failed: %v", callID, err)
		srv.mu.Lock()
		delete(srv.pendingCalls, calleeNum)
		srv.mu.Unlock()
		snd(&ocnserverpb.CallEvent{CallId: callID, Type: ocnserverpb.CallEvent_BUSY, Reason: "callee unavailable"})
		srv.removeInLeg(callID)
		return ""
	}

	snd(&ocnserverpb.CallEvent{CallId: callID, Type: ocnserverpb.CallEvent_RINGING})
	log.Printf("Bridge call %s queued for offline callee %s", callID, calleeNum)
	return callID
}

// handleNetService answers a hosted 800/900 service call over the bridge.
func (srv *Server) handleNetService(snd func(*ocnserverpb.CallEvent) error, ev *ocnserverpb.CallEvent, callID string, leg *inLeg, svc services.Service) bool {
	sendICE := func(c webrtc.ICECandidateInit) {
		snd(&ocnserverpb.CallEvent{
			CallId:    callID,
			Type:      ocnserverpb.CallEvent_ICE,
			Candidate: iceInitToCommon(c),
		})
	}

	answer, err := svc.HandleCall(callID, sdpToWebRTC(ev.Sdp), sendICE)
	if err != nil {
		log.Printf("Bridge service %s failed: %v", callID, err)
		snd(&ocnserverpb.CallEvent{CallId: callID, Type: ocnserverpb.CallEvent_BUSY, Reason: "service error"})
		return false
	}
	if err := snd(&ocnserverpb.CallEvent{
		CallId: callID,
		Type:   ocnserverpb.CallEvent_ANSWER,
		Sdp:    &commonpb.SDPSession{Sdp: answer.SDP, Type: answer.Type.String()},
	}); err != nil {
		log.Printf("Bridge service %s answer send failed: %v", callID, err)
		return false
	}
	log.Printf("Bridge service %s answered (%s)", callID, leg.calleeNum)
	return true
}

func (srv *Server) removeInLeg(callID string) {
	srv.fedMu.Lock()
	delete(srv.inLegs, callID)
	srv.fedMu.Unlock()
}

// inboundRemoteHangup handles the remote caller hanging up / timing out.
func (srv *Server) inboundRemoteHangup(callID, reason string) {
	srv.fedMu.Lock()
	leg := srv.inLegs[callID]
	if leg != nil {
		delete(srv.inLegs, callID)
	}
	srv.fedMu.Unlock()
	if leg == nil {
		srv.removePendingByCallID(callID)
		return
	}
	if leg.service != nil {
		_ = leg.service.EndCall(callID)
		return
	}
	if leg.callee != nil {
		leg.callee.mu.Lock()
		if leg.callee.callID == callID {
			leg.callee.callID = ""
		}
		leg.callee.mu.Unlock()
		srv.sendMessage(leg.callee, &ServerMessage{
			CallEnded: &CallEnded{CallID: callID, Reason: orDefault(reason, "hangup")},
		})
	} else {
		// Was still pending/offline.
		srv.removePendingByCallID(callID)
	}
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// ---- conversions between signaling SDP/ICE and the wire types ----

func sdpToCommon(s *SDPSession) *commonpb.SDPSession {
	if s == nil {
		return nil
	}
	return &commonpb.SDPSession{Sdp: s.SDP, Type: s.Type}
}

func commonToSdp(s *commonpb.SDPSession) *SDPSession {
	if s == nil {
		return nil
	}
	return &SDPSession{SDP: s.Sdp, Type: s.Type}
}

func iceToCommon(c *ICECandidate) *commonpb.ICECandidate {
	if c == nil {
		return nil
	}
	return &commonpb.ICECandidate{Candidate: c.Candidate, SdpMid: c.SDPMid, SdpMlineIndex: c.SDPMLineIndex}
}

func sdpToWebRTC(s *commonpb.SDPSession) *webrtc.SessionDescription {
	typ := webrtc.SDPTypeOffer
	if s != nil && s.Type == "answer" {
		typ = webrtc.SDPTypeAnswer
	}
	if s == nil {
		return &webrtc.SessionDescription{Type: typ, SDP: ""}
	}
	return &webrtc.SessionDescription{Type: typ, SDP: s.Sdp}
}

func commonToICEInit(c *commonpb.ICECandidate) webrtc.ICECandidateInit {
	out := webrtc.ICECandidateInit{Candidate: c.Candidate}
	mid := c.SdpMid
	mline := uint16(c.SdpMlineIndex)
	out.SDPMid = &mid
	out.SDPMLineIndex = &mline
	return out
}

func iceInitToCommon(c webrtc.ICECandidateInit) *commonpb.ICECandidate {
	out := &commonpb.ICECandidate{Candidate: c.Candidate}
	if c.SDPMid != nil {
		out.SdpMid = *c.SDPMid
	}
	if c.SDPMLineIndex != nil {
		out.SdpMlineIndex = int32(*c.SDPMLineIndex)
	}
	return out
}
