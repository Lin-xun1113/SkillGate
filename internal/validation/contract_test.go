package validation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindSecretFieldIsDeterministicAndDetectsValue(t *testing.T) {
	value := map[string]any{"z": "safe", "a": map[string]any{"token": "hidden"}}
	if got := FindSecretField(value, ""); got != "a.token" {
		t.Fatalf("path = %q", got)
	}
}

func TestResolveReferenceRejectsTraversalAbsoluteAndSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := ResolveReference(root, root, "../outside.txt"); code != "PATH_OUTSIDE_ROOT" {
		t.Fatalf("traversal code = %q", code)
	}
	if _, code := ResolveReference(root, root, outside); code != "PATH_OUTSIDE_ROOT" {
		t.Fatalf("absolute code = %q", code)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, code := ResolveReference(root, root, "link"); code != "PATH_OUTSIDE_ROOT" {
		t.Fatalf("symlink code = %q", code)
	}
	linkDir := filepath.Join(root, "linkdir")
	if err := os.Symlink(t.TempDir(), linkDir); err != nil {
		t.Fatal(err)
	}
	if _, code := ResolveReference(root, root, "linkdir/file"); code != "PATH_OUTSIDE_ROOT" {
		t.Fatalf("intermediate symlink code = %q", code)
	}
}
