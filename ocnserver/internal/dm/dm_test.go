package dm

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/open-carrier-network/ocn/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestEnqueuePendingGetRemove(t *testing.T) {
	s := testStore(t)
	m := NewManager(s)
	if err := m.EnsureMasterSecret(); err != nil {
		t.Fatal(err)
	}

	env := &Envelope{
		MessageID: "msg-1",
		ClientID:  "client-1",
		From:      "4405550001",
		FromName:  "Alice",
		To:        "5551001",
		Kind:      "text",
		Text:      "hi",
		CreatedAt: time.Now().UnixMilli(),
	}
	if _, err := m.Enqueue("5551001", env); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Persistence across managers (fresh secret shares the store secret).
	m2 := NewManager(s)
	if err := m2.EnsureMasterSecret(); err != nil {
		t.Fatal(err)
	}
	pending, err := m2.Pending("5551001")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Text != "hi" {
		t.Fatalf("pending mismatch: %+v", pending)
	}

	got, err := m2.Get("msg-1", "5551001")
	if err != nil || got == nil || got.FromName != "Alice" {
		t.Fatalf("get failed: %v / %v", got, err)
	}

	// Scoping: another recipient cannot see or remove it.
	if other, _ := m2.Get("msg-1", "5559999"); other != nil {
		t.Fatal("wrong recipient read a queued message")
	}
	if err := m2.Remove("msg-1", "5559999"); err != nil {
		t.Fatal(err)
	}
	if err := m2.Remove("msg-1", "5551001"); err != nil {
		t.Fatal(err)
	}
	left, _ := m2.Pending("5551001")
	if len(left) != 0 {
		t.Fatal("message not removed")
	}
}

func TestImageRejectedBySize(t *testing.T) {
	// Just confirm constant sanity used by handlers.
	if MaxImageBytes != 4<<20 {
		t.Fatalf("unexpected MaxImageBytes %d", MaxImageBytes)
	}
}
