package store

import (
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"testing"
	"time"
)

func newTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "reg.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestAllowedArea(t *testing.T) {
	cases := map[string]bool{
		"800":  false,
		"900":  false,
		"000":  false,
		"199":  false,
		"999":  true,
		"212":  true,
		"310":  true,
		"555":  true,
		"200":  true,
		"12a":  false,
		"80":   false,
		"8000": false,
	}
	for code, want := range cases {
		if got := AllowedArea(code); got != want {
			t.Errorf("AllowedArea(%q)=%v want %v", code, got, want)
		}
	}
}

func TestRegisterFixedAndReserved(t *testing.T) {
	s := newTest(t)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)

	code, err := s.RegisterServer(&ServerInfo{AreaCode: "212", Name: "a", ServerAddr: "x:1", PublicKey: pub})
	if err != nil || code != "212" {
		t.Fatalf("register 212: %v %q", err, code)
	}

	// Duplicate (different key) rejected.
	pub2, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := s.RegisterServer(&ServerInfo{AreaCode: "212", Name: "b", ServerAddr: "x:2", PublicKey: pub2}); err == nil {
		t.Fatal("expected ErrAreaTaken for duplicate 212")
	}

	// Same server re-registering (same key) is allowed and keeps its code.
	code2, err := s.RegisterServer(&ServerInfo{AreaCode: "212", Name: "a2", ServerAddr: "x:1b", PublicKey: pub})
	if err != nil || code2 != "212" {
		t.Fatalf("same-key re-register should succeed: %v %q", err, code2)
	}

	// Reserved
	if _, err := s.RegisterServer(&ServerInfo{AreaCode: "800", Name: "c", ServerAddr: "x:3", PublicKey: pub}); err == nil {
		t.Fatal("expected 800 to be rejected")
	}
	if _, err := s.RegisterServer(&ServerInfo{AreaCode: "900", Name: "c", ServerAddr: "x:3", PublicKey: pub}); err == nil {
		t.Fatal("expected 900 to be rejected")
	}
}

func TestRegisterAutoAndRoute(t *testing.T) {
	s := newTest(t)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)

	code, err := s.RegisterServer(&ServerInfo{Name: "auto", ServerAddr: "h:1", PublicKey: pub})
	if err != nil {
		t.Fatalf("auto register: %v", err)
	}
	if !AllowedArea(code) {
		t.Fatalf("auto code %q not allowed", code)
	}
	info, err := s.GetRoute(code)
	if err != nil || info == nil {
		t.Fatalf("route %q missing: %v", code, err)
	}
	if _, err := s.GetRoute("997"); err == nil {
		t.Fatal("expected ErrServerNotFound for unknown code")
	}
}

func TestAuthSignVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	ts := time.Now().Unix()

	// Empty/expired token? verify window: ts in past 5 min is fine.
	sig := SignAuth(priv, "212", ts)
	if !ValidAuth(pub, "212", ts, sig) {
		t.Fatal("valid signature rejected")
	}
	if ValidAuth(pub, "213", ts, sig) {
		t.Fatal("signature accepted for wrong area code")
	}
	bad := append([]byte{}, sig...)
	bad[0] ^= 0xff
	if ValidAuth(pub, "212", ts, bad) {
		t.Fatal("corrupted signature accepted")
	}
	// Signature over stale timestamp (10 min ago) should fail.
	old := time.Now().Add(-10 * time.Minute).Unix()
	if ValidAuth(pub, "212", old, SignAuth(priv, "212", old)) {
		t.Fatal("stale timestamp accepted")
	}
}
