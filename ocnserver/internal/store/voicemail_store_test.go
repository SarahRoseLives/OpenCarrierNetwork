package store

import (
	"path/filepath"
	"testing"
)

func TestVoicemailStoreCRUD(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	id, err := s.StoreVoicemail("5551234", "4405550001", "Alice", []byte{1, 2, 3}, "opus", 12)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, err := s.StoreVoicemail("5551234", "4405550002", "Bob", []byte{4, 5}, "opus", 5); err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, err := s.StoreVoicemail("5559999", "4405550001", "Alice", []byte{6}, "opus", 1); err != nil {
		t.Fatalf("store: %v", err)
	}

	meta, err := s.ListVoicemailMeta("5551234")
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) != 2 {
		t.Fatalf("want 2 messages, got %d", len(meta))
	}

	got, err := s.GetVoicemail(id, "5551234")
	if err != nil || got == nil {
		t.Fatalf("get: %v / %v", got, err)
	}
	if !bytesEq(got.EncryptedAudio, []byte{1, 2, 3}) {
		t.Fatal("audio mismatch")
	}

	unread, _ := s.CountUnlistened("5551234")
	if unread != 2 {
		t.Fatalf("want 2 unread, got %d", unread)
	}
	if err := s.MarkListened(id, "5551234"); err != nil {
		t.Fatal(err)
	}
	unread, _ = s.CountUnlistened("5551234")
	if unread != 1 {
		t.Fatalf("want 1 unread after mark, got %d", unread)
	}

	// Wrong recipient cannot fetch or delete.
	if got, _ := s.GetVoicemail(id, "5559999"); got != nil {
		t.Fatal("wrong recipient fetched a message")
	}
	if err := s.DeleteVoicemail(id, "5559999"); err != nil {
		t.Fatal(err)
	}
	if n := s.countRecipient(t, "5551234"); n != 2 {
		t.Fatalf("delete by wrong recipient removed a message (got %d)", n)
	}
	if err := s.DeleteVoicemail(id, "5551234"); err != nil {
		t.Fatal(err)
	}
	if n := s.countRecipient(t, "5551234"); n != 1 {
		t.Fatalf("delete failed (got %d)", n)
	}
}

func (s *Store) countRecipient(t *testing.T, recipient string) int {
	t.Helper()
	meta, err := s.ListVoicemailMeta(recipient)
	if err != nil {
		t.Fatal(err)
	}
	return len(meta)
}

func bytesEq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
