package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillPackageHashPreservesFrontmatterTextOrder(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	leftText := "---\nname: demo\ndescription: demo\n---\nbody\n"
	rightText := "---\ndescription: demo\nname: demo\n---\nbody\n"
	for root, content := range map[string]string{left: leftText, right: rightText} {
		if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	first, err := SkillPackageHash(left)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SkillPackageHash(right)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("frontmatter text order unexpectedly normalized: %s", first)
	}
}
