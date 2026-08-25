package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalStringMatrix(t *testing.T) {
	cases := []struct {
		name        string
		left, right any
		same        bool
	}{
		{"key-order", map[string]any{"b": 1, "a": 2}, map[string]any{"a": 2, "b": 1}, true},
		{"newline", "a\r\nb", "a\nb", true},
		{"unicode", "销售额-😀", "销售额-😀", true},
		{"different", "one", "two", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			left, _ := HashCanonical(tc.left)
			right, _ := HashCanonical(tc.right)
			if (left == right) != tc.same {
				t.Fatalf("hashes %s/%s same=%v want=%v", left, right, left == right, tc.same)
			}
		})
	}
}

func TestSkillPackageRejectsAuxiliarySymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: demo\ndescription: demo\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "notes.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := SkillPackageHash(root); err == nil {
		t.Fatal("expected auxiliary symlink rejection")
	}
}
