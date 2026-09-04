package voicemail

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestBoxRoundtrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	secret := []byte("the quick brown fox records a voicemail")
	blob, err := BoxEncrypt(key, secret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	got, err := BoxDecrypt(key, blob)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(got, secret) {
		t.Fatal("roundtrip mismatch")
	}
	// Tamper resistance.
	if _, err := BoxDecrypt(key, append(append([]byte{}, blob...), 0)); err == nil {
		t.Fatal("expected decrypt to fail on tampered blob")
	}
}

func TestDeriveMailboxKeyDiffers(t *testing.T) {
	master := []byte("master-secret-0123456789abcdef")
	a := DeriveMailboxKey(master, "5551234")
	b := DeriveMailboxKey(master, "5559999")
	c := DeriveMailboxKey([]byte("other-master"), "5551234")
	if bytes.Equal(a, b) {
		t.Fatal("keys for different numbers must differ")
	}
	if bytes.Equal(a, c) {
		t.Fatal("keys for different masters must differ")
	}
}

func TestSerializeFramesRoundtrip(t *testing.T) {
	frames := [][]byte{{1, 2, 3}, {}, {9, 8, 7, 6, 5}}
	blob := SerializeFrames(frames)
	got := DeserializeFrames(blob)
	if len(got) != len(frames) {
		t.Fatalf("got %d frames, want %d", len(got), len(frames))
	}
	for i := range frames {
		if !bytes.Equal(got[i], frames[i]) {
			t.Fatalf("frame %d mismatch", i)
		}
	}
}

func TestBuildOggStructure(t *testing.T) {
	frames := make([][]byte, 5)
	for i := range frames {
		frames[i] = bytes.Repeat([]byte{byte(i)}, 80+i)
	}
	ogg, err := BuildOgg(frames)
	if err != nil {
		t.Fatalf("build ogg: %v", err)
	}
	if !bytes.HasPrefix(ogg, []byte("OggS")) {
		t.Fatal("output does not start with OggS")
	}
	// Header(2) + one page per audio frame.
	n := countSub(ogg, []byte("OggS"))
	want := 2 + len(frames)
	if n != want {
		t.Fatalf("expected %d Ogg pages, found %d", want, n)
	}
}

func countSub(hay, needle []byte) int {
	n := 0
	for i := 0; i+len(needle) <= len(hay); i++ {
		if bytes.Equal(hay[i:i+len(needle)], needle) {
			n++
		}
	}
	return n
}
