package services

import "github.com/pion/webrtc/v3"

type Service interface {
	Code() string
	Name() string
	HandleCall(callID string, offer *webrtc.SessionDescription, sendICE func(webrtc.ICECandidateInit)) (*webrtc.SessionDescription, error)
	HandleICE(candidate webrtc.ICECandidateInit) error
	EndCall(callID string) error
}
