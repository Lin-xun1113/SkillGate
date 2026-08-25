package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileCASRegisterSkillIsIdempotentAndImmutable(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	packageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(packageRoot, "SKILL.md"), []byte("---\nname: demo\ndescription: demo skill\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := store.RegisterSkill("demo", packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.RegisterSkill("demo", packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash != second.Hash || !second.Idempotent {
		t.Fatalf("expected idempotent registration: %#v %#v", first, second)
	}
	got, err := store.GetSkill("demo", first.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hash != first.Hash || got.Name != "demo" {
		t.Fatalf("unexpected stored version: %#v", got)
	}
}

func TestFileCASDifferentContentCreatesVersion(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, description := range []string{"one", "two"} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: demo\ndescription: "+description+"\n---\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := store.RegisterSkill("demo", root); err != nil {
			t.Fatal(err)
		}
	}
	versions, err := store.ListSkills("demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 {
		t.Fatalf("versions = %d, want 2", len(versions))
	}
}
