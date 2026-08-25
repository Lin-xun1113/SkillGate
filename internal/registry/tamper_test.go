package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillRegistrationDetectsTamperedCanonicalBlob(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	pkg := t.TempDir()
	if err := os.WriteFile(filepath.Join(pkg, "SKILL.md"), []byte("---\nname: demo\ndescription: demo\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	version, err := store.RegisterSkill("demo", pkg)
	if err != nil {
		t.Fatal(err)
	}
	blob := filepath.Join(root, "skills", "demo", version.Hash, "canonical.json")
	if err := os.WriteFile(blob, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RegisterSkill("demo", pkg); err == nil {
		t.Fatal("expected tamper conflict")
	}
}
