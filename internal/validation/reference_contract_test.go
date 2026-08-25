package validation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"gopkg.in/yaml.v3"
)

func TestValidateSuiteChecksOutputAndSecurityReferences(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(root, "input.csv")
	schema := filepath.Join(root, "schema.json")
	expected := filepath.Join(root, "expected.json")
	for path, content := range map[string]string{fixture: "a,b\n1,2\n", schema: `{"type":"object"}`, expected: `{"answer":1}`} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	badSuite := map[string]any{"apiVersion": "skillgate.dev/v1alpha1", "kind": "EvalSuite", "metadata": map[string]any{"name": "demo"}, "spec": map[string]any{"cases": []any{map[string]any{"id": "case-1", "evaluationMode": "security_probe", "population": "answer", "fixtures": []any{map[string]any{"path": "input.csv", "sha256": identity.HashBytes([]byte("a,b\n1,2\n"))}}, "outputs": []any{map[string]any{"path": "../escape.json"}}, "assertions": []any{map[string]any{"schemaRef": "missing-schema.json", "expectedRef": "expected.json", "expectedRefHash": "sha256:bad"}}, "security": map[string]any{"evidence": map[string]any{"findingRef": "missing-finding.json", "traceRef": "trace.json", "evidenceRef": "evidence.json"}}}}}}
	data, err := yaml.Marshal(badSuite)
	if err != nil {
		t.Fatal(err)
	}
	suite := filepath.Join(root, "suite.yaml")
	if err := os.WriteFile(suite, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, diagnostics := ValidateSuite(suite, root)
	codes := map[string]bool{}
	for _, diagnostic := range diagnostics {
		codes[diagnostic.Code] = true
	}
	if !codes["PATH_OUTSIDE_ROOT"] || !codes["FILE_NOT_FOUND"] || !codes["CONTENT_HASH_MISMATCH"] {
		t.Fatalf("diagnostic codes = %#v", codes)
	}
}

func TestAnswerKeyReferenceIsRejectedOutsideAgentVisibleList(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(root, "input.csv")
	if err := os.WriteFile(fixture, []byte("a,b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{"apiVersion": "skillgate.dev/v1alpha1", "kind": "EvalSuite", "metadata": map[string]any{"name": "demo"}, "spec": map[string]any{"cases": []any{map[string]any{"id": "case-1", "evaluationMode": "forced_injection", "population": "answer", "prompt": "read expected/answer.json", "fixtures": []any{map[string]any{"path": "input.csv", "sha256": identity.HashBytes([]byte("a,b\n"))}}}}}}
	data, _ := yaml.Marshal(doc)
	suite := filepath.Join(root, "suite.yaml")
	if err := os.WriteFile(suite, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, diagnostics := ValidateSuite(suite, root)
	found := false
	for _, d := range diagnostics {
		if d.Code == "LEAKAGE_DETECTED" {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}
