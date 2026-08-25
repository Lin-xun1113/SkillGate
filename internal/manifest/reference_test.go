package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateGraderReferencesRejectsMissingSchema(t *testing.T) {
	root := t.TempDir()
	grader := filepath.Join(root, "grader.yaml")
	if err := os.WriteFile(grader, []byte("apiVersion: skillgate.dev/v1alpha1\nkind: DeterministicGraderSet\nspec:\n  suiteRef: suite.yaml\n  schemaRefs:\n    - missing.json\n  expectedRefs:\n    - expected.json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "suite.yaml"), []byte("apiVersion: skillgate.dev/v1alpha1\nkind: EvalSuite\nmetadata:\n  name: demo\nspec:\n  cases: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "expected.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	diagnostics := validateGraderReferences(grader, root)
	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "FILE_NOT_FOUND" {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}
