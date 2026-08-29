package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicCASWritePublishesReadableBlob(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "canonical.json")
	if err := atomicCASWrite(path, []byte(`{"ok":true}`)); err != nil {
		t.Fatalf("atomicCASWrite: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat CAS blob: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("CAS blob mode = %o, want 644 for unprivileged readers", got)
	}
}
