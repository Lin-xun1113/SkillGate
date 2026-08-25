package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterSkillReturnsTypedValidationErrorForLogicalNameMismatch(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: actual\ndescription: demo\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = store.RegisterSkill("requested", root)
	if err == nil || DiagnosticCode(err) != "INVALID_SKILL_MANIFEST" {
		t.Fatalf("err=%v code=%q", err, DiagnosticCode(err))
	}
}
