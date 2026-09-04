// Package voicemail implements recording, storage, and retrieval of
// voicemail messages. Audio is stored at rest encrypted with a per-recipient
// key derived from a server-side master secret (see Manager.EnsureMasterSecret),
// so the server can decrypt to play messages when phone-box playback arrives.
package voicemail

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
)

// DeriveMailboxKey returns the per-recipient AES key for number, derived from
// the server master secret.
func DeriveMailboxKey(master []byte, number string) []byte {
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte(number))
	return mac.Sum(nil)
}

// BoxEncrypt seals data with AES-256-GCM. The returned blob is
// nonce(12) || ciphertext.
func BoxEncrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nil, nonce, data, nil)
	return append(nonce, sealed...), nil
}

// BoxDecrypt opens a blob produced by BoxEncrypt.
func BoxDecrypt(key, blob []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(blob) < gcm.NonceSize()+gcm.Overhead() {
		return nil, errors.New("voicemail blob too short")
	}
	nonce, ct := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]
	open, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt voicemail: %w", err)
	}
	return open, nil
}
