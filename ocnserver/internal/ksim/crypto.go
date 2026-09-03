package ksim

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"

	"golang.org/x/crypto/pbkdf2"
)

const (
	saltLen   = 16
	keyLen    = 32 // AES-256
	nonceLen  = 12 // GCM nonce
	iterCount = 100000
)

var ErrDecryptionFailed = errors.New("decryption failed")

// EncryptKey encrypts a private key with a passphrase using AES-256-GCM
// Output: salt (16) + nonce (12) + ciphertext + tag
func EncryptKey(data []byte, passphrase string) ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}

	key := deriveKey(passphrase, salt)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, nonce, data, nil)

	// Pack: salt + nonce + ciphertext (includes GCM tag)
	result := make([]byte, 0, saltLen+nonceLen+len(ciphertext))
	result = append(result, salt...)
	result = append(result, nonce...)
	result = append(result, ciphertext...)
	return result, nil
}

// DecryptKey decrypts a private key with a passphrase
func DecryptKey(encrypted []byte, passphrase string) ([]byte, error) {
	if len(encrypted) < saltLen+nonceLen+16 {
		return nil, ErrDecryptionFailed
	}

	salt := encrypted[:saltLen]
	nonce := encrypted[saltLen : saltLen+nonceLen]
	ciphertext := encrypted[saltLen+nonceLen:]

	key := deriveKey(passphrase, salt)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

func deriveKey(passphrase string, salt []byte) []byte {
	return pbkdf2.Key([]byte(passphrase), salt, iterCount, keyLen, sha256.New)
}
