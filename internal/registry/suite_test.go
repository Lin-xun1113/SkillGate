package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileCASRegisterSuiteIsIdempotent(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	suite := filepath.Join(t.TempDir(), "suite.yaml")
	if err := os.WriteFile(suite, []byte("apiVersion: skillgate.dev/v1alpha1\nkind: EvalSuite\nmetadata:\n  name: demo\nspec:\n  split: tune\n  cases: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := store.RegisterSuite("demo", suite)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.RegisterSuite("demo", suite)
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash != second.Hash || !second.Idempotent {
		t.Fatalf("expected idempotent suite registration: %#v %#v", first, second)
	}
}
