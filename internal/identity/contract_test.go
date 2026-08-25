package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillPackageCanonicalEncodesNulAsBinary(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: demo\ndescription: demo\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data.bin"), []byte{'a', 0, 'b'}, 0o644); err != nil {
		t.Fatal(err)
	}
	canonical, err := SkillPackageCanonical(root)
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) == "" || !containsString(string(canonical), `"encoding":"base64"`) {
		t.Fatalf("binary file was not base64 encoded: %s", canonical)
	}
}

func TestValidateSkillPackageRejectsMissingSkillAndSecret(t *testing.T) {
	missing := t.TempDir()
	if err := ValidateSkillPackage(missing); err == nil {
		t.Fatal("expected missing SKILL.md error")
	}
	secret := t.TempDir()
	content := "---\nname: demo\ndescription: sk-1234567890abcdef\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(secret, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSkillPackage(secret); err == nil {
		t.Fatal("expected secret-like value error")
	}
}

func containsString(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
