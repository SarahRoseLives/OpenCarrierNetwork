package ksim

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerate(t *testing.T) {
	k, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	if len(k.PublicKey) != PublicKeySize {
		t.Errorf("public key size = %d, want %d", len(k.PublicKey), PublicKeySize)
	}
	if len(k.PrivateKey) != PrivateKeySize {
		t.Errorf("private key size = %d, want %d", len(k.PrivateKey), PrivateKeySize)
	}
}

func TestSignVerify(t *testing.T) {
	k, _ := Generate()
	msg := []byte("hello ocn")
	sig := k.Sign(msg)

	if !Verify(k.PublicKey, msg, sig) {
		t.Error("Verify failed for valid signature")
	}

	if Verify(k.PublicKey, []byte("wrong message"), sig) {
		t.Error("Verify passed for wrong message")
	}

	k2, _ := Generate()
	if Verify(k2.PublicKey, msg, sig) {
		t.Error("Verify passed with wrong public key")
	}
}

func TestSignChallenge(t *testing.T) {
	k, _ := Generate()
	nonce, _ := GenerateChallenge()
	ts := time.Now().Unix()

	sig := k.SignChallenge(nonce, ts)
	if err := VerifyChallenge(k.PublicKey, nonce, ts, sig); err != nil {
		t.Fatalf("VerifyChallenge() error: %v", err)
	}

	// Wrong timestamp
	if err := VerifyChallenge(k.PublicKey, nonce, ts+1, sig); err == nil {
		t.Error("VerifyChallenge passed with wrong timestamp")
	}

	// Wrong nonce
	nonce2, _ := GenerateChallenge()
	if err := VerifyChallenge(k.PublicKey, nonce2, ts, sig); err == nil {
		t.Error("VerifyChallenge passed with wrong nonce")
	}
}

func TestFromPrivateKey(t *testing.T) {
	k, _ := Generate()
	k2, err := FromPrivateKey(k.PrivateKey)
	if err != nil {
		t.Fatalf("FromPrivateKey() error: %v", err)
	}

	if !k.PublicKey.Equal(k2.PublicKey) {
		t.Error("public keys don't match after reconstruction")
	}

	msg := []byte("test")
	sig := k.Sign(msg)
	if !Verify(k2.PublicKey, msg, sig) {
		t.Error("signature from original doesn't verify with reconstructed key")
	}
}

func TestEncodeDecodePublicKey(t *testing.T) {
	k, _ := Generate()
	encoded := k.EncodePublicKey()

	decoded, err := DecodePublicKey(encoded)
	if err != nil {
		t.Fatalf("DecodePublicKey() error: %v", err)
	}

	if !k.PublicKey.Equal(decoded) {
		t.Error("decoded public key doesn't match original")
	}
}

func TestEncodeDecodePrivateKey(t *testing.T) {
	k, _ := Generate()
	encoded := k.EncodePrivateKey()

	decoded, err := DecodePrivateKey(encoded)
	if err != nil {
		t.Fatalf("DecodePrivateKey() error: %v", err)
	}

	if !k.PrivateKey.Equal(decoded) {
		t.Error("decoded private key doesn't match original")
	}
}

func TestEncryptDecryptKey(t *testing.T) {
	k, _ := Generate()
	passphrase := "test-passphrase-123"

	encrypted, err := EncryptKey(k.PrivateKey, passphrase)
	if err != nil {
		t.Fatalf("EncryptKey() error: %v", err)
	}

	decrypted, err := DecryptKey(encrypted, passphrase)
	if err != nil {
		t.Fatalf("DecryptKey() error: %v", err)
	}

	if !ed25519.PrivateKey(decrypted).Equal(k.PrivateKey) {
		t.Error("decrypted key doesn't match original")
	}

	// Wrong passphrase
	_, err = DecryptKey(encrypted, "wrong-passphrase")
	if err == nil {
		t.Error("DecryptKey passed with wrong passphrase")
	}
}

func TestSaveLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ksim")
	passphrase := "secure-passphrase"

	k, _ := Generate()
	err := k.SaveFile(path, "Alice", "server-a.ocn.network", "212-555-1234", passphrase)
	if err != nil {
		t.Fatalf("SaveFile() error: %v", err)
	}

	// Verify file exists and is not world-readable
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions = %o, want 0600", info.Mode().Perm())
	}

	k2, meta, err := LoadFile(path, passphrase)
	if err != nil {
		t.Fatalf("LoadFile() error: %v", err)
	}

	if !k.PublicKey.Equal(k2.PublicKey) {
		t.Error("loaded public key doesn't match original")
	}
	if !k.PrivateKey.Equal(k2.PrivateKey) {
		t.Error("loaded private key doesn't match original")
	}
	if meta.DisplayName != "Alice" {
		t.Errorf("display name = %q, want %q", meta.DisplayName, "Alice")
	}
	if meta.Server != "server-a.ocn.network" {
		t.Errorf("server = %q, want %q", meta.Server, "server-a.ocn.network")
	}
	if meta.Number != "212-555-1234" {
		t.Errorf("number = %q, want %q", meta.Number, "212-555-1234")
	}

	// Wrong passphrase
	_, _, err = LoadFile(path, "wrong")
	if err == nil {
		t.Error("LoadFile passed with wrong passphrase")
	}
}
