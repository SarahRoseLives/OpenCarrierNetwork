package store

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestProvisionUserLifecycle(t *testing.T) {
	s := newTestStore(t)

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tokenHash := HashToken("secret-token")

	if err := s.NewProvisionToken(tokenHash, "1234567", "Test", "note", "admin", time.Hour); err != nil {
		t.Fatalf("NewProvisionToken: %v", err)
	}

	// Consume it.
	num, err := s.ProvisionUser(tokenHash, pub, "", "Test")
	if err != nil {
		t.Fatalf("ProvisionUser: %v", err)
	}
	if num != "1234567" {
		t.Fatalf("expected fixed number, got %q", num)
	}

	// A second use of the same token must fail.
	_, err = s.ProvisionUser(tokenHash, pub, "", "Test")
	if !errors.Is(err, ErrTokenUsed) {
		t.Fatalf("expected ErrTokenUsed, got %v", err)
	}
}

func TestProvisionUserAutoAssignAndExpiry(t *testing.T) {
	s := newTestStore(t)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)

	// Auto-assign token (no fixed number)
	if err := s.NewProvisionToken(HashToken("auto"), "", "", "", "admin", time.Hour); err != nil {
		t.Fatal(err)
	}
	num, err := s.ProvisionUser(HashToken("auto"), pub, "", "")
	if err != nil {
		t.Fatalf("auto assign: %v", err)
	}
	if len(num) != 7 {
		t.Fatalf("expected 7-digit number, got %q", num)
	}

	// Expired token
	if err := s.NewProvisionToken(HashToken("expired"), "2233445", "", "", "admin", -time.Minute); err != nil {
		t.Fatal(err)
	}
	_, err = s.ProvisionUser(HashToken("expired"), pub, "", "")
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}

func TestAdminAuthAndSessions(t *testing.T) {
	s := newTestStore(t)

	must, err := s.VerifyAdminLogin("admin", "admin")
	if err != nil {
		t.Fatalf("default creds should work: %v", err)
	}
	if !must {
		t.Fatal("expected must_change on first login")
	}

	if _, err := s.VerifyAdminLogin("admin", "wrong"); err == nil {
		t.Fatal("expected bad password to fail")
	}

	if err := s.ChangeAdminPassword("admin", "admin", "hunter2"); err != nil {
		t.Fatalf("change pw: %v", err)
	}
	if _, err := s.VerifyAdminLogin("admin", "admin"); err == nil {
		t.Fatal("old password should no longer work")
	}
	if _, err := s.VerifyAdminLogin("admin", "hunter2"); err != nil {
		t.Fatalf("new password should work: %v", err)
	}

	sess, err := s.CreateSession("admin")
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.SessionUsername(sess)
	if err != nil || user != "admin" {
		t.Fatalf("session lookup: %q %v", user, err)
	}
	if err := s.DeleteSession(sess); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUsername(sess); err == nil {
		t.Fatal("deleted session should not validate")
	}
}

func TestRandomFreeNumbers(t *testing.T) {
	s := newTestStore(t)
	nums, err := s.RandomFreeNumbers(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(nums) != 10 {
		t.Fatalf("expected 10 free numbers, got %d", len(nums))
	}
	seen := map[string]bool{}
	for _, n := range nums {
		if seen[n] {
			t.Fatalf("duplicate free number %s", n)
		}
		seen[n] = true
	}
}
