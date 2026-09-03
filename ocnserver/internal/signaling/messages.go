package signaling

import (
	"github.com/open-carrier-network/ocn/internal/numbers"
)

// ClientMessage represents a message from client to exchange
type ClientMessage struct {
	ChallengeRequest *ChallengeRequest    `json:"challenge_request,omitempty"`
	Register         *RegisterRequest     `json:"register,omitempty"`
	RegisterFCM      *RegisterFCM         `json:"register_fcm,omitempty"`
	Call             *CallRequest         `json:"call,omitempty"`
	CallAnswer       *CallAnswer          `json:"call_answer,omitempty"`
	CallHangup       *CallHangup          `json:"call_hangup,omitempty"`
	ICECandidate     *ICECandidateTrickle `json:"ice_candidate,omitempty"`
	Ping             *Ping                `json:"ping,omitempty"`
}

// ServerMessage represents a message from exchange to client
type ServerMessage struct {
	ChallengeResponse *ChallengeResponseMsg `json:"challenge_response,omitempty"`
	RegisterResponse  *RegisterResponse     `json:"register_response,omitempty"`
	IncomingCall      *IncomingCall         `json:"incoming_call,omitempty"`
	CallRinging       *CallRinging          `json:"call_ringing,omitempty"`
	CallConnected     *CallConnected        `json:"call_connected,omitempty"`
	CallEnded         *CallEnded            `json:"call_ended,omitempty"`
	ICECandidate      *ICECandidateTrickle  `json:"ice_candidate,omitempty"`
	Error             *Error                `json:"error,omitempty"`
	Pong              *Pong                 `json:"pong,omitempty"`
}

type ChallengeRequest struct {
	KsimID *KSimID `json:"ksim_id"`
}

type ChallengeResponseMsg struct {
	Nonce     []byte `json:"nonce"`
	Timestamp int64  `json:"timestamp"`
}

type RegisterRequest struct {
	KsimID            *KSimID            `json:"ksim_id"`
	ChallengeResponse *ChallengeResponse `json:"challenge_response"`
	DisplayName       *DisplayName       `json:"display_name"`
	ActivationToken   string             `json:"activation_token,omitempty"`
}

type RegisterResponse struct {
	Success        bool         `json:"success"`
	AssignedNumber *PhoneNumber `json:"assigned_number,omitempty"`
	ErrorMessage   string       `json:"error_message,omitempty"`
	IceServers     []IceServer  `json:"ice_servers,omitempty"`
}

// IceServer is a WebRTC ICE server handed to clients (STUN/TURN).
type IceServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type RegisterFCM struct {
	Token string `json:"token"`
}

type CallRequest struct {
	Destination string      `json:"destination"`
	Offer       *SDPSession `json:"offer"`
}

type IncomingCall struct {
	CallID       string       `json:"call_id"`
	CallerNumber *PhoneNumber `json:"caller_number"`
	CallerName   *DisplayName `json:"caller_name"`
	Offer        *SDPSession  `json:"offer"`
}

type CallAnswer struct {
	CallID string      `json:"call_id"`
	Answer *SDPSession `json:"answer"`
}

type CallRinging struct {
	CallID string `json:"call_id"`
}

type CallConnected struct {
	CallID  string       `json:"call_id"`
	Answer  *SDPSession  `json:"answer,omitempty"`
	Service *ServiceInfo `json:"service,omitempty"`
}

type ServiceInfo struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type CallEnded struct {
	CallID string `json:"call_id"`
	Reason string `json:"reason"`
}

type CallHangup struct {
	CallID string `json:"call_id"`
}

type ICECandidateTrickle struct {
	CallID    string        `json:"call_id"`
	Candidate *ICECandidate `json:"candidate"`
}

type Ping struct{}
type Pong struct{}

type Error struct {
	Code    int32  `json:"code"`
	Message string `json:"message"`
}

// Shared types
type KSimID struct {
	PublicKey []byte `json:"public_key"`
}

type ChallengeResponse struct {
	Signature []byte `json:"signature"`
}

type DisplayName struct {
	Name string `json:"name"`
}

type PhoneNumber struct {
	AreaCode string `json:"area_code"`
	Number   string `json:"number"`
}

func (p *PhoneNumber) Full() string {
	return numbers.FormatNumber(p.AreaCode, p.Number)
}

func (p *PhoneNumber) Local() string {
	return numbers.FormatLocal(p.Number)
}

type SDPSession struct {
	SDP  string `json:"sdp"`
	Type string `json:"type"` // "offer" or "answer"
}

type ICECandidate struct {
	Candidate     string `json:"candidate"`
	SDPMid        string `json:"sdp_mid"`
	SDPMLineIndex int32  `json:"sdp_mline_index"`
}
