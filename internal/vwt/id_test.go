package vwt

import (
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestGenerateID_Format(t *testing.T) {
	now := time.Date(2026, 3, 1, 2, 3, 4, 0, time.UTC)
	id, err := GenerateID(now)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !strings.HasPrefix(id, "20260301-020304-") {
		t.Fatalf("unexpected prefix: %q", id)
	}
	if len(id) != len("20260301-020304-00000000") {
		t.Fatalf("unexpected len=%d id=%q", len(id), id)
	}
	sfx := id[len("20260301-020304-"):]
	for i := 0; i < len(sfx); i++ {
		c := sfx[i]
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		if !isHex {
			t.Fatalf("non-hex suffix: %q", id)
		}
	}
}

func TestGenerateID_RandReadError(t *testing.T) {
	old := rand.Reader
	t.Cleanup(func() { rand.Reader = old })
	rand.Reader = failingReader{}

	if _, err := GenerateID(time.Now()); err == nil {
		t.Fatalf("expected error")
	}
}

type failingReader struct{}

func (failingReader) Read(p []byte) (int, error) {
	return 0, errors.New("boom")
}

func TestRefs(t *testing.T) {
	if got := PatchRef("x"); got != "refs/vwt/patches/x" {
		t.Fatalf("got=%q", got)
	}
	if got := SnapshotRef("y"); got != "refs/vwt/snapshots/y" {
		t.Fatalf("got=%q", got)
	}
}
