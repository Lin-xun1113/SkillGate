package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAPICursorRoundTrip(t *testing.T) {
	want := apiCursor{CreatedAt: time.Date(2026, 8, 28, 1, 2, 3, 0, time.UTC), ID: "trial-1"}
	encoded := encodeCursor(want)
	got, err := decodeCursor(encoded)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if got.ID != want.ID || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("cursor mismatch: got %#v want %#v", got, want)
	}
	if _, err := decodeCursor("not-a-cursor"); err == nil {
		t.Fatal("expected malformed cursor to fail")
	}
}

func TestSecureArtifactPathRejectsEscapeAndSymlink(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "inside.txt")
	if err := os.WriteFile(inside, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{artifactsRoot: root}
	resolved, err := s.secureArtifactPath("inside.txt")
	wantResolved, _ := filepath.EvalSymlinks(inside)
	if err != nil || resolved != wantResolved {
		t.Fatalf("inside path rejected: %v (%s)", err, resolved)
	}
	if _, err := s.secureArtifactPath("../outside.txt"); err == nil {
		t.Fatal("expected path traversal to fail")
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := s.secureArtifactPath("link.txt"); err == nil {
		t.Fatal("expected symlink escape to fail")
	}
}
