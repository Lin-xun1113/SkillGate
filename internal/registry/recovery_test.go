package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIdempotentRegistrationRebuildsMissingIndex(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	pkg := t.TempDir()
	if err := os.WriteFile(filepath.Join(pkg, "SKILL.md"), []byte("---\nname: demo\ndescription: demo\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RegisterSkill("demo", pkg); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "index.json")); err != nil {
		t.Fatal(err)
	}
	store, err = NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	version, err := store.RegisterSkill("demo", pkg)
	if err != nil || !version.Idempotent {
		t.Fatalf("register err=%v version=%#v", err, version)
	}
	if _, err := os.Stat(filepath.Join(root, "index.json")); err != nil {
		t.Fatalf("index was not rebuilt: %v", err)
	}
}
