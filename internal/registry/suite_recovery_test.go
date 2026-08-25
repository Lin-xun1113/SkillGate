package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIdempotentSuiteRegistrationRebuildsMissingIndex(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	suitePath := filepath.Join(t.TempDir(), "suite.yaml")
	if err := os.WriteFile(suitePath, []byte("apiVersion: skillgate.dev/v1alpha1\nkind: EvalSuite\nmetadata:\n  name: demo\nspec:\n  cases: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RegisterSuite("demo", suitePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "index.json")); err != nil {
		t.Fatal(err)
	}
	store, err = NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	version, err := store.RegisterSuite("demo", suitePath)
	if err != nil || !version.Idempotent {
		t.Fatalf("err=%v version=%#v", err, version)
	}
	if _, err := os.Stat(filepath.Join(root, "index.json")); err != nil {
		t.Fatalf("missing rebuilt index: %v", err)
	}
}
