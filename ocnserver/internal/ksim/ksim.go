package ksim

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

var (
	ErrInvalidKeySize    = errors.New("invalid key size")
	ErrInvalidSignature  = errors.New("invalid signature")
	ErrInvalidKSimFile   = errors.New("invalid ksim file")
	ErrChallengeExpired  = errors.New("challenge expired")
)

const (
	PublicKeySize  = ed25519.PublicKeySize  // 32 bytes
	PrivateKeySize = ed25519.PrivateKeySize // 64 bytes
	ChallengeTTL   = 5 * time.Minute
)

// KSim represents a keypair-based identity
type KSim struct {
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

// KSimFile is the on-disk format for a kSIM identity
type KSimFile struct {
	Version       int        `json:"version"`
	PublicKey     string     `json:"public_key"`     // base64
	PrivateKey    string     `json:"private_key"`    // base64 (encrypted)
	DisplayName   string     `json:"display_name"`
	Server        string     `json:"server"`
	Number        string     `json:"number"`
	RegisteredAt  time.Time  `json:"registered_at"`
}

// Generate creates a new Ed25519 keypair
func Generate() (*KSim, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating keypair: %w", err)
	}
	return &KSim{
		PublicKey:  pub,
		PrivateKey: priv,
	}, nil
}

// FromPrivateKey reconstructs a KSim from a private key
func FromPrivateKey(privKeyBytes []byte) (*KSim, error) {
	if len(privKeyBytes) != PrivateKeySize {
		return nil, ErrInvalidKeySize
	}
	priv := ed25519.PrivateKey(privKeyBytes)
	pub := priv.Public().(ed25519.PublicKey)
	return &KSim{
		PublicKey:  pub,
		PrivateKey: priv,
	}, nil
}

// Sign signs arbitrary data with the private key
func (k *KSim) Sign(data []byte) []byte {
	return ed25519.Sign(k.PrivateKey, data)
}

// Verify checks a signature against the public key
func Verify(publicKey ed25519.PublicKey, data, signature []byte) bool {
	return ed25519.Verify(publicKey, data, signature)
}

// SignChallenge signs a challenge nonce + timestamp
func (k *KSim) SignChallenge(nonce []byte, timestamp int64) []byte {
	data := buildChallengeData(nonce, timestamp)
	return k.Sign(data)
}

// VerifyChallenge verifies a challenge response
func VerifyChallenge(publicKey ed25519.PublicKey, nonce []byte, timestamp int64, signature []byte) error {
	if time.Since(time.Unix(timestamp, 0)) > ChallengeTTL {
		return ErrChallengeExpired
	}
	data := buildChallengeData(nonce, timestamp)
	if !Verify(publicKey, data, signature) {
		return ErrInvalidSignature
	}
	return nil
}

func buildChallengeData(nonce []byte, timestamp int64) []byte {
	h := sha256.New()
	h.Write(nonce)
	ts := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		ts[i] = byte(timestamp)
		timestamp >>= 8
	}
	h.Write(ts)
	return h.Sum(nil)
}

// GenerateChallenge creates a random challenge nonce
func GenerateChallenge() ([]byte, error) {
	nonce := make([]byte, 32)
	_, err := rand.Read(nonce)
	return nonce, err
}

// EncodePublicKey returns base64-encoded public key
func (k *KSim) EncodePublicKey() string {
	return base64.StdEncoding.EncodeToString(k.PublicKey)
}

// EncodePrivateKey returns base64-encoded private key
func (k *KSim) EncodePrivateKey() string {
	return base64.StdEncoding.EncodeToString(k.PrivateKey)
}

// DecodePublicKey decodes a base64 public key
func DecodePublicKey(encoded string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	if len(b) != PublicKeySize {
		return nil, ErrInvalidKeySize
	}
	return ed25519.PublicKey(b), nil
}

// DecodePrivateKey decodes a base64 private key
func DecodePrivateKey(encoded string) (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	if len(b) != PrivateKeySize {
		return nil, ErrInvalidKeySize
	}
	return ed25519.PrivateKey(b), nil
}

// SaveFile writes a kSIM file to disk (private key encrypted with passphrase)
func (k *KSim) SaveFile(path string, displayName, server, number string, passphrase string) error {
	encPrivKey, err := EncryptKey(k.PrivateKey, passphrase)
	if err != nil {
		return fmt.Errorf("encrypting private key: %w", err)
	}

	f := KSimFile{
		Version:      1,
		PublicKey:    k.EncodePublicKey(),
		PrivateKey:   base64.StdEncoding.EncodeToString(encPrivKey),
		DisplayName:  displayName,
		Server:       server,
		Number:       number,
		RegisteredAt: time.Now(),
	}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling ksim file: %w", err)
	}

	return os.WriteFile(path, data, 0600)
}

// LoadFile reads a kSIM file from disk and decrypts the private key
func LoadFile(path string, passphrase string) (*KSim, *KSimFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("reading ksim file: %w", err)
	}

	var f KSimFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, nil, ErrInvalidKSimFile
	}

	if f.Version != 1 {
		return nil, nil, fmt.Errorf("unsupported ksim file version: %d", f.Version)
	}

	encPrivKeyBytes, err := base64.StdEncoding.DecodeString(f.PrivateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("decoding private key: %w", err)
	}

	privKeyBytes, err := DecryptKey(encPrivKeyBytes, passphrase)
	if err != nil {
		return nil, nil, fmt.Errorf("decrypting private key: %w", err)
	}

	ksim, err := FromPrivateKey(privKeyBytes)
	if err != nil {
		return nil, nil, err
	}

	return ksim, &f, nil
}
