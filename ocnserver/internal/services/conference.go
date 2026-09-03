package services

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
)

const (
	mixSampleRate = 48000
	mixFrameSize  = 960 // 20ms @48kHz mono
	mixFrameMS    = 20
)

// ConferenceService is a hosted party-line: every participant's audio is
// decoded, mixed (excluding their own), re-encoded, and sent to the others.
type ConferenceService struct {
	tts    *TTS
	name   string
	phrase string

	mu      sync.Mutex
	parts   map[string]*confPart
	enc     *OpusEncoder
	seq     uint16
	ts      uint32
	started bool
}

type confPart struct {
	id      string
	pc      *webrtc.PeerConnection
	track   *webrtc.TrackLocalStaticRTP
	active  bool
	frame   []int16 // latest decoded 20ms frame
	frameMu sync.Mutex
	done    chan struct{}
}

func NewConferenceService(tts *TTS, name, phrase string) (*ConferenceService, error) {
	enc, err := NewOpusEncoder()
	if err != nil {
		return nil, fmt.Errorf("opus encoder: %w", err)
	}
	return &ConferenceService{
		tts:    tts,
		name:   name,
		phrase: phrase,
		parts:  make(map[string]*confPart),
		enc:    enc,
	}, nil
}

func (c *ConferenceService) Code() string { return "" }
func (c *ConferenceService) Name() string { return c.name }

// HandleCall adds a participant to the room.
func (c *ConferenceService) HandleCall(callID string, offer *webrtc.SessionDescription, sendICE func(webrtc.ICECandidateInit)) (*webrtc.SessionDescription, error) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	})
	if err != nil {
		return nil, fmt.Errorf("new peer connection: %w", err)
	}
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 1},
		"party-audio", "party-room",
	)
	if err != nil {
		pc.Close()
		return nil, fmt.Errorf("new track: %w", err)
	}
	sender, err := pc.AddTrack(track)
	if err != nil {
		pc.Close()
		return nil, fmt.Errorf("add track: %w", err)
	}
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, err := sender.Read(buf); err != nil {
				return
			}
		}
	}()

	p := &confPart{
		id:    callID,
		pc:    pc,
		track: track,
		done:  make(chan struct{}),
	}

	pc.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand != nil {
			sendICE(cand.ToJSON())
		}
	})
	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		go c.receiveAudio(p, remote)
	})

	c.mu.Lock()
	c.parts[callID] = p
	first := !c.started
	c.started = true
	c.mu.Unlock()
	if first {
		go c.mixer()
	}

	if err := pc.SetRemoteDescription(*offer); err != nil {
		pc.Close()
		c.remove(callID)
		return nil, fmt.Errorf("set remote description: %w", err)
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		pc.Close()
		c.remove(callID)
		return nil, fmt.Errorf("create answer: %w", err)
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		pc.Close()
		c.remove(callID)
		return nil, fmt.Errorf("set local description: %w", err)
	}

	// Play the welcome message on the newcomer's own stream, then enable mixing
	// (avoids concurrent writes to the same track).
	go c.welcome(p)

	log.Printf("Conference %s: participant %s joined", c.name, callID)
	return &answer, nil
}

func (c *ConferenceService) receiveAudio(p *confPart, remote *webrtc.TrackRemote) {
	dec, err := NewOpusDecoder()
	if err != nil {
		log.Printf("Conference %s: decoder create: %v", c.name, err)
		return
	}
	pcm := make([]int16, mixFrameSize)
	bad := 0
	first := true
	for {
		select {
		case <-p.done:
			return
		default:
		}
		pkt, _, err := remote.ReadRTP()
		if err != nil {
			return
		}
		if pkt.PayloadType != 111 {
			continue
		}
		n, err := dec.Decode(pkt.Payload, pcm)
		if err != nil {
			bad++
			if bad == 1 || bad%200 == 0 {
				log.Printf("Conference %s: decode err count %d: %v", c.name, bad, err)
			}
			continue
		}
		if n != mixFrameSize {
			continue
		}
		cp := make([]int16, mixFrameSize)
		copy(cp, pcm)
		p.frameMu.Lock()
		p.frame = cp
		p.frameMu.Unlock()
		if first {
			first = false
			log.Printf("Conference %s: first decoded audio frame for %s", c.name, p.id)
		}
	}
}

func (c *ConferenceService) welcome(p *confPart) {
	frames, err := c.tts.GenerateOpusFrames(c.phrase)
	if err != nil {
		log.Printf("Conference %s: tts error: %v", c.name, err)
	} else {
		var seq uint16
		var ts uint32
		for _, f := range frames {
			select {
			case <-p.done:
				return
			default:
			}
			pkt := &rtp.Packet{
				Header:  rtp.Header{Version: 2, PayloadType: 111, SequenceNumber: seq, Timestamp: ts, SSRC: 999001, Marker: true},
				Payload: f,
			}
			if err := p.track.WriteRTP(pkt); err != nil {
				return
			}
			seq++
			ts += mixFrameSize
			time.Sleep(mixFrameMS * time.Millisecond)
		}
	}
	c.mu.Lock()
	p.active = true
	c.mu.Unlock()
	log.Printf("Conference %s: participant %s active", c.name, p.id)
}

// mixer runs every 20ms: mix active participants' latest frames and send each
// participant a mix that excludes their own contribution.
func (c *ConferenceService) mixer() {
	ticker := time.NewTicker(mixFrameMS * time.Millisecond)
	defer ticker.Stop()
	encBuf := make([]byte, 1500)
	for range ticker.C {
		c.mu.Lock()
		var active []*confPart
		for _, p := range c.parts {
			if p.active {
				active = append(active, p)
			}
		}
		if len(active) == 0 {
			c.mu.Unlock()
			continue
		}

		// Snapshot each participant's latest frame.
		mix := make([][]int16, len(active))
		for i, p := range active {
			p.frameMu.Lock()
			if p.frame != nil {
				cp := make([]int16, mixFrameSize)
				copy(cp, p.frame)
				mix[i] = cp
			}
			p.frameMu.Unlock()
		}

		var seq uint16
		var ts uint32
		_ = seq
		_ = ts
		seq = c.seq
		ts = c.ts
		c.seq++
		c.ts += mixFrameSize
		ssrc := uint32(777001)

		// Encode a mix for each receiver, excluding that receiver's own audio.
		for i, receiver := range active {
			var hasOther bool
			mixed := make([]int16, mixFrameSize)
			for j, src := range mix {
				if j == i || src == nil {
					continue
				}
				hasOther = true
				for k := 0; k < mixFrameSize; k++ {
					s := int(mixed[k]) + int(src[k])
					if s > 32767 {
						s = 32767
					} else if s < -32768 {
						s = -32768
					}
					mixed[k] = int16(s)
				}
			}
			if !hasOther {
				continue // silence; nothing to send
			}
			n, err := c.enc.Encode(mixed, encBuf)
			if err != nil {
				continue
			}
			pkt := &rtp.Packet{
				Header:  rtp.Header{Version: 2, PayloadType: 111, SequenceNumber: seq, Timestamp: ts, SSRC: ssrc, Marker: true},
				Payload: append([]byte(nil), encBuf[:n]...),
			}
			if err := receiver.track.WriteRTP(pkt); err != nil {
				log.Printf("Conference %s: write to %s: %v", c.name, receiver.id, err)
			}
		}
		c.mu.Unlock()
	}
}

// HandleCallICE routes a candidate to one participant.
func (c *ConferenceService) HandleCallICE(callID string, candidate webrtc.ICECandidateInit) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.parts[callID]
	if p == nil {
		return nil
	}
	return p.pc.AddICECandidate(candidate)
}

func (c *ConferenceService) HandleICE(candidate webrtc.ICECandidateInit) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range c.parts {
		if err := p.pc.AddICECandidate(candidate); err != nil {
			return err
		}
	}
	return nil
}

func (c *ConferenceService) remove(callID string) {
	c.mu.Lock()
	if p, ok := c.parts[callID]; ok {
		delete(c.parts, callID)
		close(p.done)
		_ = p.pc.Close()
	}
	c.mu.Unlock()
}

func (c *ConferenceService) EndCall(callID string) error {
	log.Printf("Conference %s: participant %s left", c.name, callID)
	c.remove(callID)
	return nil
}
