package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileRejectsMissingTopLevelSkill(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	source, err := os.ReadFile(filepath.Join(root, "experiments/csv-analysis-v1-demo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(string(source), "  skills:\n    - name: csv-analysis\n      path: ./skills/csv-analysis\n      version: sha256:5a2153eaf0a11af8d6141b87755526503e02a3af99080464cae27f49e5164090\n", "  skills: []\n", 1)
	path := filepath.Join(root, "experiments/.m1-missing-skill.yaml")
	if err := os.WriteFile(path, []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	_, diagnostics := Compile(path, root)
	found := false
	for _, d := range diagnostics {
		if d.Code == "PAIR_IDENTITY_MISMATCH" {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics=%#v", diagnostics)
	}
}

func TestCompileRejectsCandidateSkillWithoutFrozenVersion(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	source, err := os.ReadFile(filepath.Join(root, "experiments/csv-analysis-v1-demo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(string(source), "        - name: csv-analysis\n          version: sha256:5a2153eaf0a11af8d6141b87755526503e02a3af99080464cae27f49e5164090", "        - name: csv-analysis", 1)
	path := filepath.Join(root, "experiments/.m1-unfrozen-skill.yaml")
	if err := os.WriteFile(path, []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	_, diagnostics := Compile(path, root)
	found := false
	for _, d := range diagnostics {
		if d.Code == "PAIR_IDENTITY_MISMATCH" {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics=%#v", diagnostics)
	}
}
