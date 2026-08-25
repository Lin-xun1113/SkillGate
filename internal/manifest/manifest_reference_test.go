package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Lin-xun1113/SkillGate/internal/validation"
)

func TestCompileRejectsMissingIdentityAndGraderReferences(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	manifestPath := filepath.Join(root, "experiments", "csv-analysis-v1-demo.yaml")
	original, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	mutated := string(original) + "\n# mutation\n"
	mutated = replaceOnce(mutated, "identityExamples: ./evals/csv-analysis/identity-examples.json", "identityExamples: ./evals/csv-analysis/missing-identity.json")
	path := filepath.Join(root, "experiments", ".m1-missing-reference.yaml")
	if err := os.WriteFile(path, []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	_, diagnostics := Compile(path, root)
	if !hasDiagnostic(diagnostics, "FILE_NOT_FOUND") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func replaceOnce(value, old, new string) string {
	for i := 0; i+len(old) <= len(value); i++ {
		if value[i:i+len(old)] == old {
			return value[:i] + new + value[i+len(old):]
		}
	}
	return value
}
func hasDiagnostic(diagnostics []validation.Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
