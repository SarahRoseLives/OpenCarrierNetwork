package voicemail

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"time"

	"github.com/open-carrier-network/ocn/internal/services"
	"github.com/open-carrier-network/ocn/internal/store"
)

// Manager coordinates voicemail storage for a server: it owns the master
// secret, encrypts/decrypts messages, and knows how to render them.
type Manager struct {
	store   *store.Store
	tts     *services.TTS
	maxDur  time.Duration
	master  []byte
	loaded  bool
	enabled bool

	// OnStored is invoked (async) after a message is persisted, so the
	// signaling server can push/refresh the recipient.
	OnStored func(recipientNumber, callerNumber, callerName string)
}

// NewManager builds a voicemail manager. enabled gates recording.
func NewManager(st *store.Store, tts *services.TTS, maxDur time.Duration, enabled bool) *Manager {
	return &Manager{store: st, tts: tts, maxDur: maxDur, enabled: enabled}
}

// Enabled reports whether voicemail recording is turned on for this server.
func (m *Manager) Enabled() bool { return m != nil && m.enabled && m.tts != nil }

func (m *Manager) MaxDuration() time.Duration {
	if m == nil || m.maxDur <= 0 {
		return 120 * time.Second
	}
	return m.maxDur
}

// EnsureMasterSecret loads or creates the server master secret.
func (m *Manager) EnsureMasterSecret() error {
	if m == nil || m.store == nil {
		return fmt.Errorf("voicemail manager not configured")
	}
	enc, err := m.store.GetSetting(MasterSecretKey)
	if err != nil {
		return err
	}
	if enc != "" {
		key, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			return fmt.Errorf("voicemail master secret corrupt: %w", err)
		}
		m.master = key
		m.loaded = true
		return nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	if err := m.store.SetSetting(MasterSecretKey, base64.StdEncoding.EncodeToString(key)); err != nil {
		return err
	}
	m.master = key
	m.loaded = true
	log.Printf("voicemail: generated new master secret")
	return nil
}

func (m *Manager) key(recipientNumber string) ([]byte, error) {
	if m.master == nil {
		if err := m.EnsureMasterSecret(); err != nil {
			return nil, err
		}
	}
	return DeriveMailboxKey(m.master, recipientNumber), nil
}

// StoreMessage encrypts and persists a recorded message.
func (m *Manager) StoreMessage(recipientNumber, callerNumber, callerName string, frames [][]byte) (string, error) {
	if len(frames) == 0 {
		return "", fmt.Errorf("no audio recorded")
	}
	key, err := m.key(recipientNumber)
	if err != nil {
		return "", err
	}
	blob, err := BoxEncrypt(key, SerializeFrames(frames))
	if err != nil {
		return "", err
	}
	dur := int32(len(frames) * frameMS / 1000)
	id, err := m.store.StoreVoicemail(recipientNumber, callerNumber, callerName, blob, "opus", dur)
	if err != nil {
		return "", err
	}
	log.Printf("voicemail: stored message %s for %s (%ds)", id, recipientNumber, dur)
	if m.OnStored != nil {
		go m.OnStored(recipientNumber, callerNumber, callerName)
	}
	return id, nil
}

// RenderOgg decrypts a stored message and returns it as a playable Ogg/Opus
// container for app playback.
func (m *Manager) RenderOgg(recipientNumber string, msg *store.Voicemail) ([]byte, error) {
	key, err := m.key(recipientNumber)
	if err != nil {
		return nil, err
	}
	plain, err := BoxDecrypt(key, msg.EncryptedAudio)
	if err != nil {
		return nil, err
	}
	frames := DeserializeFrames(plain)
	if len(frames) == 0 {
		return nil, fmt.Errorf("message %s has no audio", msg.ID)
	}
	return BuildOgg(frames)
}

// Greeting returns the TTS frames spoken to someone leaving a message.
func (m *Manager) Greeting() ([][]byte, error) {
	if m.tts == nil {
		return nil, fmt.Errorf("tts not configured")
	}
	return m.tts.GenerateOpusFrames("The person you called is not available. Please leave a message after the tone. When you are finished, hang up.")
}
