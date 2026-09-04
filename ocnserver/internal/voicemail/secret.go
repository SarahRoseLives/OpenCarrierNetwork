package voicemail

import (
	"github.com/open-carrier-network/ocn/internal/crypt"
)

// MasterSecretKey is the settings key holding the server master secret.
const MasterSecretKey = crypt.MasterSecretKey

// DeriveMailboxKey returns the per-recipient AES key for number, derived from
// the server master secret.
func DeriveMailboxKey(master []byte, number string) []byte {
	return crypt.DeriveKey(master, number)
}

// BoxEncrypt seals data with AES-256-GCM. The returned blob is
// nonce(12) || ciphertext.
func BoxEncrypt(key, data []byte) ([]byte, error) {
	return crypt.Encrypt(key, data)
}

// BoxDecrypt opens a blob produced by BoxEncrypt.
func BoxDecrypt(key, blob []byte) ([]byte, error) {
	return crypt.Decrypt(key, blob)
}
