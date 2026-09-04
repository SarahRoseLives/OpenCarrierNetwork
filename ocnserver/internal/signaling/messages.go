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
	CallDecline      *CallDecline         `json:"call_decline,omitempty"`
	ICECandidate     *ICECandidateTrickle `json:"ice_candidate,omitempty"`
	Ping             *Ping                `json:"ping,omitempty"`

	// Voicemail retrieval (visual voicemail)
	VoicemailList     *VoicemailListReq     `json:"voicemail_list,omitempty"`
	VoicemailGet      *VoicemailGetReq      `json:"voicemail_get,omitempty"`
	VoicemailDelete   *VoicemailDeleteReq   `json:"voicemail_delete,omitempty"`
	VoicemailMarkRead *VoicemailMarkReadReq `json:"voicemail_mark_read,omitempty"`

	// Direct messaging
	DMSend          *DMSend          `json:"dm_send,omitempty"`
	DMAck           *DMAck           `json:"dm_ack,omitempty"`
	DMAttachmentGet *DMAttachmentGet `json:"dm_attachment_get,omitempty"`
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

	// Voicemail responses / events
	VoicemailListResponse *VoicemailListResponse `json:"voicemail_list_response,omitempty"`
	VoicemailGetResponse  *VoicemailGetResponse  `json:"voicemail_get_response,omitempty"`
	VoicemailEvent        *VoicemailEvent        `json:"voicemail_event,omitempty"`

	// Direct messaging responses / events
	DMEvent      *DMEvent      `json:"dm_event,omitempty"`
	DMAttachment *DMAttachment `json:"dm_attachment,omitempty"`
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

// CallDecline marks an incoming call as explicitly declined (routes the
// caller to voicemail).
type CallDecline struct {
	CallID string `json:"call_id"`
}

type ICECandidateTrickle struct {
	CallID    string        `json:"call_id"`
	Candidate *ICECandidate `json:"candidate"`
}

type Ping struct{}
type Pong struct{}

// Voicemail list request (empty body).
type VoicemailListReq struct{}

// VoicemailInfo describes one stored message for the list.
type VoicemailInfo struct {
	ID              string `json:"id"`
	CallerNumber    string `json:"caller_number"` // canonical digits
	CallerName      string `json:"caller_name"`
	DurationSeconds int32  `json:"duration_seconds"`
	Listened        bool   `json:"listened"`
	CreatedAt       int64  `json:"created_at"` // unix seconds
}

type VoicemailListResponse struct {
	Messages []VoicemailInfo `json:"messages"`
	Unread   int             `json:"unread"`
}

type VoicemailGetReq struct {
	ID string `json:"id"`
}

// VoicemailGetResponse returns a message plus its audio (base64 Ogg/Opus).
type VoicemailGetResponse struct {
	ID          string `json:"id"`
	AudioBase64 string `json:"audio_b64"`
}

type VoicemailDeleteReq struct {
	ID string `json:"id"`
}

type VoicemailMarkReadReq struct {
	ID string `json:"id"`
}

// VoicemailEvent is pushed to a connected recipient when their mailbox
// changes (e.g. a new message arrived).
type VoicemailEvent struct {
	Action       string `json:"action"` // "new"
	CallerNumber string `json:"caller_number"`
	CallerName   string `json:"caller_name"`
	Unread       int    `json:"unread"`
}

// ---- Direct messaging ----

// DMImageReq is an inline image attachment being sent.
type DMImageReq struct {
	Name string `json:"name"`
	Mime string `json:"mime"`
	B64  string `json:"b64"`
}

// DMSend asks the server to deliver a 1:1 message to To (canonical number).
type DMSend struct {
	To       string      `json:"to"`
	ClientID string      `json:"client_id,omitempty"`
	Kind     string      `json:"kind"` // "text" | "image"
	Text     string      `json:"text,omitempty"`
	Image    *DMImageReq `json:"image,omitempty"`
}

// DMAck confirms a delivered message was received; the server drops its
// outbox copy.
type DMAck struct {
	MessageID string `json:"message_id"`
}

// DMAttachmentGet requests the bytes of an image message.
type DMAttachmentGet struct {
	MessageID string `json:"message_id"`
}

// DMEvent is pushed to a device. Type "new" is an inbound message; type
// "status" acknowledges an outbound message reached the network.
type DMEvent struct {
	Type      string `json:"type"` // "new" | "status"
	MessageID string `json:"message_id"`
	ClientID  string `json:"client_id,omitempty"`
	Status    string `json:"status,omitempty"` // "delivered"
	From      string `json:"from,omitempty"`
	FromName  string `json:"from_name,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Text      string `json:"text,omitempty"`
	ImageName string `json:"image_name,omitempty"`
	ImageMime string `json:"image_mime,omitempty"`
	CreatedAt int64  `json:"created_at,omitempty"` // unix millis
}

// DMAttachment is the payload reply to DMAttachmentGet.
type DMAttachment struct {
	MessageID string `json:"message_id"`
	Name      string `json:"name"`
	Mime      string `json:"mime"`
	B64       string `json:"b64"`
}

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
