package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestFieldMutationsReject(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	source, err := os.ReadFile(filepath.Join(root, "experiments/csv-analysis-v1-demo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, needle, replacement, code string }{
		{"model", "name: deterministic-agent-v1", "name: mutation-agent-v1", "PAIR_IDENTITY_MISMATCH"},
		{"tool-policy", "filesystem.write", "network", "PAIR_IDENTITY_MISMATCH"},
		{"grader-hash", "sha256:b5f94acfd374f9c92123e4e25006aacd4bf703aefab72dc23a9f00b98f779892", "sha256:0000000000000000000000000000000000000000000000000000000000000000", "CONTENT_HASH_MISMATCH"},
		{"environment-hash", "sha256:32626be493e0395b64e6275d5202819e4d1c8bdd91ea33eaae187819e21d073e", "sha256:0000000000000000000000000000000000000000000000000000000000000000", "CONTENT_HASH_MISMATCH"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := strings.Replace(string(source), tc.needle, tc.replacement, 1)
			path := filepath.Join(root, "experiments", ".m1-field-"+tc.name+".yaml")
			if err := os.WriteFile(path, []byte(mutated), 0o644); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(path)
			_, diagnostics := Compile(path, root)
			found := false
			for _, d := range diagnostics {
				if d.Code == tc.code {
					found = true
				}
			}
			if !found {
				t.Fatalf("want %s diagnostics=%#v", tc.code, diagnostics)
			}
		})
	}
}

func TestManifestRejectsProviderRuntimeFields(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	source, err := os.ReadFile(filepath.Join(root, "experiments/csv-analysis-v1-demo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		injection string
		field     string
	}{
		{name: "base-url", injection: "base_url: https://untrusted.example/v1", field: "base_url"},
		{name: "endpoint", injection: "endpoint: https://untrusted.example/v1", field: "endpoint"},
		{name: "headers", injection: "headers:\n            Authorization: attacker", field: "headers"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutated := strings.ReplaceAll(
				string(source),
				"          temperature: 0",
				"          temperature: 0\n          "+test.injection,
			)
			path := filepath.Join(root, "experiments", ".provider-runtime-"+test.name+".yaml")
			if err := os.WriteFile(path, []byte(mutated), 0o644); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(path)

			_, diagnostics := Compile(path, root)
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == "PROVIDER_RUNTIME_FIELD_FORBIDDEN" && strings.Contains(diagnostic.Path, test.field) {
					return
				}
			}
			t.Fatalf("expected PROVIDER_RUNTIME_FIELD_FORBIDDEN for %s, diagnostics=%#v", test.field, diagnostics)
		})
	}
}
