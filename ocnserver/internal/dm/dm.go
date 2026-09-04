// Package dm implements 1:1 direct messaging between OCN numbers. Servers
// relay messages to an online peer or queue them (encrypted at rest) on the
// recipient's home server until the device acks. History itself is kept
// on-device; the server only holds undelivered outbox entries.
package dm

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/open-carrier-network/ocn/internal/crypt"
	"github.com/open-carrier-network/ocn/internal/store"
)

// MaxImageBytes caps inline image payloads.
const MaxImageBytes = 4 << 20 // 4 MiB (raw bytes; base64 is larger)

// DM TTL for undelivered queued messages.
const queueTTL = 7 * 24 * time.Hour

// Image is an inline image attachment.
type Image struct {
	Name string `json:"name"`
	Mime string `json:"mime"`
	B64  string `json:"b64"`
}

// Envelope is a message being delivered between servers / to a device.
type Envelope struct {
	MessageID string `json:"message_id"`
	ClientID  string `json:"client_id,omitempty"`
	From      string `json:"from"` // canonical digits
	FromName  string `json:"from_name,omitempty"`
	To        string `json:"to"`   // canonical digits
	Kind      string `json:"kind"` // "text" | "image"
	Text      string `json:"text,omitempty"`
	Image     *Image `json:"image,omitempty"`
	CreatedAt int64  `json:"created_at"` // unix millis
}

// Manager seals/unseals and persists undelivered messages for one server.
type Manager struct {
	store  *store.Store
	master []byte
	loaded bool
}

func NewManager(st *store.Store) *Manager {
	return &Manager{store: st}
}

// EnsureMasterSecret loads (or creates) the shared master secret. Reuses the
// same settings key as voicemail so all at-rest scopes share one secret.
func (m *Manager) EnsureMasterSecret() error {
	if m == nil || m.store == nil {
		return fmt.Errorf("dm manager not configured")
	}
	enc, err := m.store.GetSetting(crypt.MasterSecretKey)
	if err != nil {
		return err
	}
	if enc != "" {
		key, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			return fmt.Errorf("master secret corrupt: %w", err)
		}
		m.master = key
		m.loaded = true
		return nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	if err := m.store.SetSetting(crypt.MasterSecretKey, base64.StdEncoding.EncodeToString(key)); err != nil {
		return err
	}
	m.master = key
	m.loaded = true
	log.Printf("dm: generated new master secret")
	return nil
}

func (m *Manager) key(recipient string) ([]byte, error) {
	if m.master == nil {
		if err := m.EnsureMasterSecret(); err != nil {
			return nil, err
		}
	}
	return crypt.DeriveKey(m.master, recipient), nil
}

func sealJSON(key []byte, env *Envelope) ([]byte, error) {
	raw, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	return crypt.Encrypt(key, raw)
}

// Enqueue seals and queues a message for a recipient (7-digit local number),
// keying the outbox row by env.MessageID so the device ack can find it.
func (m *Manager) Enqueue(recipient string, env *Envelope) (string, error) {
	key, err := m.key(recipient)
	if err != nil {
		return "", err
	}
	blob, err := sealJSON(key, env)
	if err != nil {
		return "", err
	}
	return env.MessageID, m.store.StoreDMPreassigned(env.MessageID, recipient, blob)
}

// EnqueueRemote stores a message that was created on another server under its
// original id, so re-delivery is idempotent.
func (m *Manager) EnqueueRemote(messageID, recipient string, env *Envelope) error {
	key, err := m.key(recipient)
	if err != nil {
		return err
	}
	blob, err := sealJSON(key, env)
	if err != nil {
		return err
	}
	return m.store.StoreDMPreassigned(messageID, recipient, blob)
}

// Pending decrypts all queued envelopes for a recipient (newest last).
func (m *Manager) Pending(recipient string) ([]*Envelope, error) {
	rows, err := m.store.ListPendingDM(recipient)
	if err != nil {
		return nil, err
	}
	key, err := m.key(recipient)
	if err != nil {
		return nil, err
	}
	out := make([]*Envelope, 0, len(rows))
	for _, r := range rows {
		plain, err := crypt.Decrypt(key, r.Envelope)
		if err != nil {
			log.Printf("dm: dropping undecryptable queued message %s: %v", r.MessageID, err)
			continue
		}
		var env Envelope
		if err := json.Unmarshal(plain, &env); err != nil {
			continue
		}
		out = append(out, &env)
	}
	return out, nil
}

// Get decrypts one queued message for a recipient (attachment fetch).
func (m *Manager) Get(messageID, recipient string) (*Envelope, error) {
	row, err := m.store.GetDM(messageID, recipient)
	if err != nil || row == nil {
		return nil, err
	}
	key, err := m.key(recipient)
	if err != nil {
		return nil, err
	}
	plain, err := crypt.Decrypt(key, row.Envelope)
	if err != nil {
		return nil, err
	}
	var env Envelope
	if err := json.Unmarshal(plain, &env); err != nil {
		return nil, err
	}
	return &env, nil
}

// Remove deletes a delivered/acked message.
func (m *Manager) Remove(messageID, recipient string) error {
	return m.store.DeleteDM(messageID, recipient)
}

// RunPruneLoop periodically drops undelivered messages older than queueTTL.
func (m *Manager) RunPruneLoop() {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		n, err := m.store.PruneDM(time.Now().Add(-queueTTL))
		if err != nil {
			log.Printf("dm: prune error: %v", err)
			continue
		}
		if n > 0 {
			log.Printf("dm: pruned %d expired queued messages", n)
		}
	}
}

// Canonical builds a canonical (digits-only) full number.
func Canonical(area, number string) string {
	if area == "" {
		return number
	}
	return area + number
}
