package voicemail

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/open-carrier-network/ocn/internal/services"
	"github.com/open-carrier-network/ocn/internal/store"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
)

// LeaveService answers an incoming call on behalf of a recipient and records
// the caller's message. It implements the signaling service contract
// structurally so it can live in the server's svcCalls map.
type LeaveService struct {
	m          *Manager
	recipient  *store.User
	callerNum  string // canonical caller number stored on the message
	callerName string
	ice        []webrtc.ICEServer // ICE/TURN servers for the media leg (fallback: Google STUN)
	mu         sync.Mutex
	calls      map[string]*leaveCall

	// OnSelfEnd, when set, is invoked after the recorder ends the call itself
	// (recording cap reached) so the signaling server can notify the caller.
	OnSelfEnd func(callID string)
}

type leaveCall struct {
	pc         *webrtc.PeerConnection
	localTrack *webrtc.TrackLocalStaticRTP
	done       chan struct{}
}

// NewLeaveService builds a voicemail recorder for a recipient.
func NewLeaveService(m *Manager, recipient *store.User, callerNumber, callerName string) *LeaveService {
	return &LeaveService{
		m:          m,
		recipient:  recipient,
		callerNum:  callerNumber,
		callerName: callerName,
		calls:      make(map[string]*leaveCall),
	}
}

// SetICEServers configures the STUN/TURN servers used for the media leg so a
// caller behind NAT can reach the recorder. Empty leaves the default STUN.
func (l *LeaveService) SetICEServers(servers []webrtc.ICEServer) {
	l.ice = servers
}

func (l *LeaveService) Code() string { return "voicemail" }
func (l *LeaveService) Name() string { return "Voicemail" }

func (l *LeaveService) HandleCall(callID string, offer *webrtc.SessionDescription, sendICE func(webrtc.ICECandidateInit)) (*webrtc.SessionDescription, error) {
	ice := l.ice
	if len(ice) == 0 {
		ice = []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}}
	}
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{ICEServers: ice})
	if err != nil {
		return nil, fmt.Errorf("voicemail: create peer: %w", err)
	}

	localTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: frameSampleRate, Channels: frameChannels},
		"voicemail-audio", "voicemail",
	)
	if err != nil {
		pc.Close()
		return nil, fmt.Errorf("voicemail: create track: %w", err)
	}
	rtpSender, err := pc.AddTrack(localTrack)
	if err != nil {
		pc.Close()
		return nil, fmt.Errorf("voicemail: add track: %w", err)
	}
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, err := rtpSender.Read(buf); err != nil {
				return
			}
		}
	}()

	call := &leaveCall{pc: pc, localTrack: localTrack, done: make(chan struct{})}
	l.mu.Lock()
	l.calls[callID] = call
	l.mu.Unlock()

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		sendICE(c.ToJSON())
	})

	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		log.Printf("voicemail: call %s ICE state -> %s", callID, state.String())
	})

	pc.OnTrack(func(remoteTrack *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		log.Printf("voicemail: call %s got inbound track (%s)", callID, remoteTrack.Codec().MimeType)
		go l.runRecordSession(callID, call, remoteTrack)
	})

	if err := pc.SetRemoteDescription(*offer); err != nil {
		l.remove(callID)
		pc.Close()
		return nil, fmt.Errorf("voicemail: set remote: %w", err)
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		l.remove(callID)
		pc.Close()
		return nil, fmt.Errorf("voicemail: create answer: %w", err)
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		l.remove(callID)
		pc.Close()
		return nil, fmt.Errorf("voicemail: set local: %w", err)
	}
	log.Printf("voicemail: answering %s for recipient %s", callID, l.recipient.Number)
	return &answer, nil
}

func (l *LeaveService) HandleICE(candidate webrtc.ICECandidateInit) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, call := range l.calls {
		_ = call.pc.AddICECandidate(candidate)
	}
	return nil
}

func (l *LeaveService) EndCall(callID string) error {
	l.mu.Lock()
	call, ok := l.calls[callID]
	delete(l.calls, callID)
	l.mu.Unlock()
	if !ok {
		return nil
	}
	log.Printf("voicemail: ending session %s", callID)
	close(call.done)
	return call.pc.Close()
}

func (l *LeaveService) remove(callID string) {
	l.mu.Lock()
	delete(l.calls, callID)
	l.mu.Unlock()
}

func (l *LeaveService) runRecordSession(callID string, call *leaveCall, remote *webrtc.TrackRemote) {
	greeting, err := l.m.Greeting()
	if err != nil {
		log.Printf("voicemail: greeting tts failed: %v", err)
		greeting = nil
	}
	if err := playFrames(call.localTrack, greeting, call.done); err != nil {
		log.Printf("voicemail: greeting play error: %v", err)
		return
	}

	maxFrames := int(l.m.MaxDuration() / (frameMS * time.Millisecond))
	if maxFrames <= 0 {
		maxFrames = 120 * 1000 / frameMS
	}

	var frames [][]byte
	dec, err := services.NewOpusDecoder()
	if err != nil {
		log.Printf("voicemail: decoder create failed: %v", err)
	} else {
		defer dec.Close()
	}
	pcm := make([]int16, frameSize)

	selfEnd := false
recordLoop:
	for len(frames) < maxFrames {
		select {
		case <-call.done:
			break recordLoop
		default:
		}
		pkt, _, err := remote.ReadRTP()
		if err != nil {
			break recordLoop
		}
		if pkt.PayloadType != 111 || len(pkt.Payload) == 0 {
			continue
		}
		cp := make([]byte, len(pkt.Payload))
		copy(cp, pkt.Payload)
		frames = append(frames, cp)
		if len(frames) >= maxFrames {
			log.Printf("voicemail: %s reached max duration", callID)
			selfEnd = true
			break recordLoop
		}
	}

	if dec != nil && len(frames) > 0 {
		trimmed := trimSilence(dec, pcm, frames)
		if len(trimmed) < len(frames) {
			log.Printf("voicemail: trimmed %d trailing silent frames", len(frames)-len(trimmed))
		}
		frames = trimmed
	}

	if len(frames) > 0 {
		if _, err := l.m.StoreMessage(l.recipient.Number, l.callerNum, l.callerName, frames); err != nil {
			log.Printf("voicemail: store failed: %v", err)
		}
	} else {
		log.Printf("voicemail: %s no audio recorded, discarding", callID)
	}

	if selfEnd {
		// Thank the caller, then end the session ourselves.
		if bye, err := l.m.tts.GenerateOpusFrames("Thank you. Goodbye."); err == nil {
			_ = playFrames(call.localTrack, bye, call.done)
		}
		l.EndCall(callID)
		if l.OnSelfEnd != nil {
			l.OnSelfEnd(callID)
		}
	}
}

// playFrames writes opus frames as RTP on a local track, paced at 20ms.
func playFrames(track *webrtc.TrackLocalStaticRTP, frames [][]byte, done chan struct{}) error {
	if track == nil || len(frames) == 0 {
		return nil
	}
	var seq uint16
	var ts uint32
	for _, frame := range frames {
		select {
		case <-done:
			return nil
		default:
		}
		pkt := &rtp.Packet{
			Header: rtp.Header{
				Version: 2, PayloadType: 111,
				SequenceNumber: seq, Timestamp: ts, SSRC: 0x564f4943, // "VOIC"
			},
			Payload: frame,
		}
		if err := track.WriteRTP(pkt); err != nil {
			return err
		}
		seq++
		ts += frameSize
		time.Sleep(frameMS * time.Millisecond)
	}
	return nil
}

// trimSilence removes trailing near-silent frames (up to 3s) so the tail of a
// message has no dead air.
func trimSilence(dec *services.OpusDecoder, pcm []int16, frames [][]byte) [][]byte {
	if len(frames) == 0 {
		return frames
	}
	keep := len(frames)
	removable := 3 * 1000 / frameMS // up to 3s
	for i := len(frames) - 1; i >= 0; i-- {
		if keep-i > removable {
			break
		}
		n, err := dec.Decode(frames[i], pcm)
		if err != nil {
			break
		}
		if !isSilent(pcm[:n]) {
			break
		}
		keep = i
	}
	if keep == 0 {
		return nil
	}
	return frames[:keep]
}

func isSilent(pcm []int16) bool {
	var peak int64
	for _, s := range pcm {
		a := int64(s)
		if a < 0 {
			a = -a
		}
		if a > peak {
			peak = a
		}
	}
	return peak < 120
}
