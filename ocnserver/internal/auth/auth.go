package auth

import (
	"crypto/ed25519"
	"sync"
	"time"

	"github.com/open-carrier-network/ocn/internal/ksim"
)

const (
	challengeTTL = 5 * time.Minute
)

type PendingChallenge struct {
	Nonce     []byte
	Timestamp int64
	CreatedAt time.Time
}

type AuthManager struct {
	challenges map[string]*PendingChallenge // key: base64 public key
	mu         sync.RWMutex
}

func NewAuthManager() *AuthManager {
	a := &AuthManager{
		challenges: make(map[string]*PendingChallenge),
	}
	go a.cleanupLoop()
	return a
}

// GenerateChallenge creates a challenge for a given public key
func (a *AuthManager) GenerateChallenge(pubKey ed25519.PublicKey) ([]byte, int64, error) {
	nonce, err := ksim.GenerateChallenge()
	if err != nil {
		return nil, 0, err
	}

	ts := time.Now().Unix()
	k := &ksim.KSim{PublicKey: pubKey}
	key := k.EncodePublicKey()

	a.mu.Lock()
	a.challenges[key] = &PendingChallenge{
		Nonce:     nonce,
		Timestamp: ts,
		CreatedAt: time.Now(),
	}
	a.mu.Unlock()

	return nonce, ts, nil
}

// VerifyResponse verifies a challenge response and returns the public key if valid
func (a *AuthManager) VerifyResponse(pubKey ed25519.PublicKey, signature []byte) error {
	k := &ksim.KSim{PublicKey: pubKey}
	key := k.EncodePublicKey()

	a.mu.RLock()
	challenge, exists := a.challenges[key]
	a.mu.RUnlock()

	if !exists {
		return ErrNoPendingChallenge
	}

	err := ksim.VerifyChallenge(pubKey, challenge.Nonce, challenge.Timestamp, signature)

	// Remove challenge after verification (one-time use)
	a.mu.Lock()
	delete(a.challenges, key)
	a.mu.Unlock()

	return err
}

// cleanupLoop removes expired challenges
func (a *AuthManager) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		a.mu.Lock()
		for key, ch := range a.challenges {
			if time.Since(ch.CreatedAt) > challengeTTL {
				delete(a.challenges, key)
			}
		}
		a.mu.Unlock()
	}
}

var ErrNoPendingChallenge = &AuthError{"no pending challenge for this key"}

type AuthError struct {
	msg string
}

func (e *AuthError) Error() string {
	return e.msg
}
