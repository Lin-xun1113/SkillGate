package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestM0ManifestMutationsReturnStableDiagnostics(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	projectRoot := filepath.Clean(filepath.Join(root, "..", ".."))
	original, err := os.ReadFile(filepath.Join(projectRoot, "experiments", "csv-analysis-v1-demo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct{ name, needle, replacement, code string }{
		{"environment", "environmentHash: sha256:32626be493e0395b64e6275d5202819e4d1c8bdd91ea33eaae187819e21d073e", "environmentHash: sha256:0000000000000000000000000000000000000000000000000000000000000000", "CONTENT_HASH_MISMATCH"},
		{"treatment", "treatment: skill_version", "treatment: old_skill", "UNSUPPORTED_TREATMENT"},
		{"candidate-model", "name: deterministic-agent-v1", "name: other-agent-v1", "PAIR_IDENTITY_MISMATCH"},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			mutated := strings.Replace(string(original), mutation.needle, mutation.replacement, 1)
			path := filepath.Join(projectRoot, "experiments", ".m1-mutation-"+mutation.name+".yaml")
			if err := os.WriteFile(path, []byte(mutated), 0o644); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(path)
			_, diagnostics := Compile(path, projectRoot)
			found := false
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == mutation.code {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected %s, diagnostics=%#v", mutation.code, diagnostics)
			}
		})
	}
}
