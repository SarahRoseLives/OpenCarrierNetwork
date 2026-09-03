package services

import (
	"fmt"
	"log"
	"sync"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
)

type EchoService struct {
	tts   *TTS
	calls map[string]*echoCall
	mu    sync.RWMutex
}

type echoCall struct {
	pc         *webrtc.PeerConnection
	localTrack *webrtc.TrackLocalStaticRTP
	done       chan struct{}
}

func NewEchoService(tts *TTS) (*EchoService, error) {
	return &EchoService{
		tts:   tts,
		calls: make(map[string]*echoCall),
	}, nil
}

func (e *EchoService) Code() string { return "*01" }
func (e *EchoService) Name() string { return "Echo Test" }

func (e *EchoService) HandleCall(callID string, offer *webrtc.SessionDescription, sendICE func(webrtc.ICECandidateInit)) (*webrtc.SessionDescription, error) {
	log.Printf("EchoService: handling call %s", callID)

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create PeerConnection: %w", err)
	}

	localTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 1},
		"echo-audio",
		"echo-service",
	)
	if err != nil {
		pc.Close()
		return nil, fmt.Errorf("failed to create track: %w", err)
	}

	rtpSender, err := pc.AddTrack(localTrack)
	if err != nil {
		pc.Close()
		return nil, fmt.Errorf("failed to add track: %w", err)
	}

	// Read RTCP (required)
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, err := rtpSender.Read(buf); err != nil {
				return
			}
		}
	}()

	call := &echoCall{
		pc:         pc,
		localTrack: localTrack,
		done:       make(chan struct{}),
	}

	e.mu.Lock()
	e.calls[callID] = call
	e.mu.Unlock()

	// Send our ICE candidates back to the caller
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		log.Printf("EchoService: sending ICE candidate to caller: %s", c.Address)
		sendICE(c.ToJSON())
	})

	// Handle incoming audio track from caller
	pc.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("EchoService: ONTRACK fired (codec: %s)", remoteTrack.Codec().MimeType)

		go func() {
			pktCount := 0
			for {
				select {
				case <-call.done:
					return
				default:
				}

				pkt, _, err := remoteTrack.ReadRTP()
				if err != nil {
					log.Printf("EchoService: read error after %d pkts: %v", pktCount, err)
					return
				}

				pktCount++
				if pktCount == 1 {
					log.Printf("EchoService: first pkt seq=%d ts=%d payload=%d bytes",
						pkt.SequenceNumber, pkt.Timestamp, len(pkt.Payload))
				}

				echoPkt := &rtp.Packet{
					Header: rtp.Header{
						Version:        2,
						PayloadType:    pkt.PayloadType,
						SequenceNumber: pkt.SequenceNumber,
						Timestamp:      pkt.Timestamp,
						SSRC:           54321,
						Marker:         pkt.Marker,
					},
					Payload: pkt.Payload,
				}

				if err := localTrack.WriteRTP(echoPkt); err != nil {
					log.Printf("EchoService: write error: %v", err)
					return
				}
			}
		}()
	})

	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		log.Printf("EchoService: ICE state %s", state.String())
	})

	// Set remote description (caller's offer)
	if err = pc.SetRemoteDescription(*offer); err != nil {
		pc.Close()
		e.mu.Lock()
		delete(e.calls, callID)
		e.mu.Unlock()
		return nil, fmt.Errorf("failed to set remote description: %w", err)
	}

	// Create answer
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		pc.Close()
		e.mu.Lock()
		delete(e.calls, callID)
		e.mu.Unlock()
		return nil, fmt.Errorf("failed to create answer: %w", err)
	}

	if err = pc.SetLocalDescription(answer); err != nil {
		pc.Close()
		e.mu.Lock()
		delete(e.calls, callID)
		e.mu.Unlock()
		return nil, fmt.Errorf("failed to set local description: %w", err)
	}

	log.Printf("EchoService: call %s ready", callID)
	return &answer, nil
}

func (e *EchoService) HandleICE(candidate webrtc.ICECandidateInit) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, call := range e.calls {
		if err := call.pc.AddICECandidate(candidate); err != nil {
			log.Printf("EchoService: failed to add ICE candidate: %v", err)
			return err
		}
		log.Printf("EchoService: added ICE candidate from caller")
	}
	return nil
}

func (e *EchoService) EndCall(callID string) error {
	e.mu.Lock()
	call, ok := e.calls[callID]
	if !ok {
		e.mu.Unlock()
		return nil
	}
	delete(e.calls, callID)
	e.mu.Unlock()

	log.Printf("EchoService: ending call %s", callID)
	close(call.done)
	return call.pc.Close()
}
