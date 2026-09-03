package services

import "github.com/pion/webrtc/v3"

type Service interface {
	Code() string
	Name() string
	HandleCall(callID string, offer *webrtc.SessionDescription, sendICE func(webrtc.ICECandidateInit)) (*webrtc.SessionDescription, error)
	HandleICE(candidate webrtc.ICECandidateInit) error
	EndCall(callID string) error
}

// CallICE is implemented by services that can route an ICE candidate to a
// specific call (needed for multi-party rooms sharing one service instance).
type CallICE interface {
	HandleCallICE(callID string, candidate webrtc.ICECandidateInit) error
}
