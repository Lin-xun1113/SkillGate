package runner

import (
	"path/filepath"
	"testing"
)

func TestResolveProjectReferenceRejectsEscape(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "experiments")
	if got := resolveProjectReference(base, root, "/etc/passwd"); got != "" {
		t.Fatalf("absolute reference resolved to %q", got)
	}
	if got := resolveProjectReference(base, root, "../../outside.yaml"); got != "" {
		t.Fatalf("parent escape resolved to %q", got)
	}
}
