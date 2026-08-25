package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateSkillPackageScansAuxiliaryFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: demo\ndescription: demo\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("token: sk-1234567890abcdef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSkillPackage(root); err == nil {
		t.Fatal("expected auxiliary secret rejection")
	}
}
