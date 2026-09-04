// Package crypt provides the shared symmetric crypto used for server-side
// encryption at rest (voicemail, messaging). Content is sealed with AES-256-GCM
// using a per-recipient key derived from a server master secret, so the server
// (which holds the secret) can decrypt when it must deliver or play content.
package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
)

// MasterSecretKey is the settings key holding the master secret shared by all
// at-rest encryption scopes (kept under its original name for compatibility).
const MasterSecretKey = "voicemail_master_secret"

// DeriveKey returns a per-scope AES key for id, derived from the master secret.
func DeriveKey(master []byte, id string) []byte {
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte(id))
	return mac.Sum(nil)
}

// Encrypt seals data; the result is nonce(12) || ciphertext.
func Encrypt(key, data []byte) ([]byte, error) {
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

// Decrypt opens a blob produced by Encrypt.
func Decrypt(key, blob []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(blob) < gcm.NonceSize()+gcm.Overhead() {
		return nil, errors.New("encrypted blob too short")
	}
	nonce, ct := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]
	open, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return open, nil
}
