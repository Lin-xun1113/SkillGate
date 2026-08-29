package grader

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRegistry(t *testing.T) {
	// Create temporary directory for test graders
	tmpDir := t.TempDir()
	registry := NewRegistry(tmpDir)

	// Create a test grader file
	graderContent := `apiVersion: skillgate.dev/v1alpha1
kind: Grader
metadata:
  name: test-json-validator
spec:
  type: deterministic
  method: json_schema
  config:
    schema_path: ./schema.json
    artifact_pattern: "output.json"
  scoring:
    passed_score: 1.0
    failed_score: 0.0
`

	graderPath := filepath.Join(tmpDir, "test-grader.yaml")
	if err := os.WriteFile(graderPath, []byte(graderContent), 0644); err != nil {
		t.Fatalf("failed to write test grader: %v", err)
	}

	// Test Register
	hash, err := registry.Register(graderPath)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if hash == "" {
		t.Fatal("Register returned empty hash")
	}

	if !startsWith(hash, "sha256:") {
		t.Errorf("hash should start with 'sha256:', got: %s", hash)
	}

	// Test Get
	grader, err := registry.Get(hash)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if grader.Metadata.Name != "test-json-validator" {
		t.Errorf("expected name 'test-json-validator', got: %s", grader.Metadata.Name)
	}

	if grader.Spec.Type != GraderTypeDeterministic {
		t.Errorf("expected type deterministic, got: %s", grader.Spec.Type)
	}

	if grader.Spec.Method != MethodJSONSchema {
		t.Errorf("expected method json_schema, got: %s", grader.Spec.Method)
	}

	// Test Validate
	if err := registry.Validate(hash); err != nil {
		t.Errorf("Validate failed: %v", err)
	}

	// Test Validate with invalid hash
	if err := registry.Validate("sha256:invalid"); err == nil {
		t.Error("Validate should fail for invalid hash")
	}

	// Test Get with invalid hash
	if _, err := registry.Get("sha256:invalid"); err == nil {
		t.Error("Get should fail for invalid hash")
	}
}

func TestLoadFromDirectoryRecursesAndSkipsNonGraders(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "csv-analysis")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	graderPath := filepath.Join(bundle, "grader.yaml")
	graderContent := `apiVersion: skillgate.dev/v1alpha1
kind: Grader
metadata:
  name: nested-grader
spec:
  type: deterministic
  method: file_exists
  config:
    artifact_pattern: output.txt
  scoring:
    passed_score: 1
    failed_score: 0
`
	if err := os.WriteFile(graderPath, []byte(graderContent), 0o644); err != nil {
		t.Fatal(err)
	}
	// EvalSuite files live in the same mounted bundle and must not be treated
	// as registry entries.
	if err := os.WriteFile(filepath.Join(bundle, "suite.yaml"), []byte("apiVersion: skillgate.dev/v1alpha1\nkind: EvalSuite\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := NewRegistry(root)
	if err := registry.LoadFromDirectory(root); err != nil {
		t.Fatalf("LoadFromDirectory failed: %v", err)
	}
	hash, err := registry.calculateHash(map[string]any{
		"apiVersion": "skillgate.dev/v1alpha1",
		"kind":       "Grader",
		"metadata":   map[string]any{"name": "nested-grader"},
		"spec": map[string]any{
			"type": "deterministic", "method": "file_exists",
			"config":  map[string]any{"artifact_pattern": "output.txt"},
			"scoring": map[string]any{"passed_score": 1, "failed_score": 0},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Get(hash); err != nil {
		t.Fatalf("nested grader was not registered: %v", err)
	}
}

func TestValidateGrader(t *testing.T) {
	registry := NewRegistry("")

	tests := []struct {
		name      string
		grader    *Grader
		wantError bool
	}{
		{
			name: "valid deterministic grader",
			grader: &Grader{
				APIVersion: "skillgate.dev/v1alpha1",
				Kind:       "Grader",
				Metadata:   GraderMetadata{Name: "test"},
				Spec: GraderSpec{
					Type:   GraderTypeDeterministic,
					Method: MethodFileContent,
				},
			},
			wantError: false,
		},
		{
			name: "missing apiVersion",
			grader: &Grader{
				Kind:     "Grader",
				Metadata: GraderMetadata{Name: "test"},
			},
			wantError: true,
		},
		{
			name: "invalid kind",
			grader: &Grader{
				APIVersion: "skillgate.dev/v1alpha1",
				Kind:       "InvalidKind",
				Metadata:   GraderMetadata{Name: "test"},
			},
			wantError: true,
		},
		{
			name: "missing name",
			grader: &Grader{
				APIVersion: "skillgate.dev/v1alpha1",
				Kind:       "Grader",
			},
			wantError: true,
		},
		{
			name: "invalid method for deterministic",
			grader: &Grader{
				APIVersion: "skillgate.dev/v1alpha1",
				Kind:       "Grader",
				Metadata:   GraderMetadata{Name: "test"},
				Spec: GraderSpec{
					Type:   GraderTypeDeterministic,
					Method: MethodLLMRubric,
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registry.validateGrader(tt.grader)
			if (err != nil) != tt.wantError {
				t.Errorf("validateGrader() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func TestDeterministicGraderSet(t *testing.T) {
	tmpDir := t.TempDir()
	registry := NewRegistry(tmpDir)

	// Create a DeterministicGraderSet file (same as evals/csv-analysis/grader.yaml)
	graderContent := `apiVersion: skillgate.dev/v1alpha1
kind: DeterministicGraderSet
metadata:
  name: csv-analysis-grader
  version: 1
spec:
  mode: deterministic_only
  llmJudge: false
  suiteRef: ./suite.yaml
  assertionSemantics: suite-assertions-v1
  schemaRefs:
    - schemas/monthly-summary.schema.json
    - schemas/insight.schema.json
  expectedRefs:
    - expected/monthly-summary.json
    - expected/insight.json
  boundary:
    expectedRoot: /grader-only/expected
    agentVisibleExpected: false
`

	graderPath := filepath.Join(tmpDir, "grader.yaml")
	if err := os.WriteFile(graderPath, []byte(graderContent), 0644); err != nil {
		t.Fatalf("failed to write test grader: %v", err)
	}

	// Test Register with DeterministicGraderSet
	hash, err := registry.Register(graderPath)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if hash == "" {
		t.Fatal("Register returned empty hash")
	}

	if !startsWith(hash, "sha256:") {
		t.Errorf("hash should start with 'sha256:', got: %s", hash)
	}

	// Test Get - should return expanded Grader
	grader, err := registry.Get(hash)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	// Verify expanded grader has correct fields
	if grader.Kind != "Grader" {
		t.Errorf("expected kind 'Grader', got: %s", grader.Kind)
	}

	if grader.Metadata.Name != "csv-analysis-grader" {
		t.Errorf("expected name 'csv-analysis-grader', got: %s", grader.Metadata.Name)
	}

	if grader.Metadata.Version != 1 {
		t.Errorf("expected version 1, got: %d", grader.Metadata.Version)
	}

	if grader.Spec.Type != GraderTypeDeterministic {
		t.Errorf("expected type deterministic, got: %s", grader.Spec.Type)
	}

	if grader.Spec.Method != MethodJSONSchema {
		t.Errorf("expected method json_schema, got: %s", grader.Spec.Method)
	}

	if grader.Spec.Mode != "deterministic_only" {
		t.Errorf("expected mode 'deterministic_only', got: %s", grader.Spec.Mode)
	}

	if grader.Spec.LLMJudge != false {
		t.Errorf("expected llmJudge false, got: %v", grader.Spec.LLMJudge)
	}

	if len(grader.Spec.SchemaRefs) != 2 {
		t.Errorf("expected 2 schema refs, got: %d", len(grader.Spec.SchemaRefs))
	}

	if len(grader.Spec.ExpectedRefs) != 2 {
		t.Errorf("expected 2 expected refs, got: %d", len(grader.Spec.ExpectedRefs))
	}
}

func TestHashMatchesManifestCompiler(t *testing.T) {
	// This test verifies that the registry's hash matches the manifest compiler's hash
	tmpDir := t.TempDir()
	registry := NewRegistry(tmpDir)

	graderContent := `apiVersion: skillgate.dev/v1alpha1
kind: Grader
metadata:
  name: test-grader
spec:
  type: deterministic
  method: json_schema
  config:
    schema_path: ./schema.json
  scoring:
    passed_score: 1.0
    failed_score: 0.0
`

	graderPath := filepath.Join(tmpDir, "grader.yaml")
	if err := os.WriteFile(graderPath, []byte(graderContent), 0644); err != nil {
		t.Fatalf("failed to write test grader: %v", err)
	}

	// Get hash from registry
	registryHash, err := registry.Register(graderPath)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Manually calculate hash using the same method as manifest compiler
	data, err := os.ReadFile(graderPath)
	if err != nil {
		t.Fatalf("failed to read grader file: %v", err)
	}

	var parsed map[string]any
	if err := unmarshalYAML(data, &parsed); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	manifestHash, err := registry.calculateHash(parsed)
	if err != nil {
		t.Fatalf("failed to calculate manifest hash: %v", err)
	}

	// Verify they match
	if registryHash != manifestHash {
		t.Errorf("hash mismatch:\n  registry: %s\n  manifest: %s", registryHash, manifestHash)
	}

	// Verify the grader can be retrieved by the hash
	grader, err := registry.Get(registryHash)
	if err != nil {
		t.Errorf("Get failed with registry hash: %v", err)
	}
	if grader == nil {
		t.Error("grader is nil")
	}
}

func unmarshalYAML(data []byte, v any) error {
	return yaml.Unmarshal(data, v)
}

func TestRealCSVAnalysisGrader(t *testing.T) {
	// Test with the actual csv-analysis grader.yaml
	graderPath := "../../evals/csv-analysis/grader.yaml"

	// Check if file exists
	if _, err := os.Stat(graderPath); os.IsNotExist(err) {
		t.Skip("csv-analysis grader.yaml not found, skipping test")
	}

	registry := NewRegistry("")

	// Test 1: Register the grader
	hash, err := registry.Register(graderPath)
	if err != nil {
		t.Fatalf("Failed to register grader: %v", err)
	}

	t.Logf("Registry hash: %s", hash)

	// Test 2: Calculate hash using manifest compiler method (identity.HashCanonical)
	data, err := os.ReadFile(graderPath)
	if err != nil {
		t.Fatalf("Failed to read grader file: %v", err)
	}

	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to parse YAML: %v", err)
	}

	manifestHash, err := registry.calculateHash(parsed)
	if err != nil {
		t.Fatalf("Failed to calculate manifest hash: %v", err)
	}

	t.Logf("Manifest hash: %s", manifestHash)

	// Test 3: Verify hashes match
	if hash != manifestHash {
		t.Errorf("Hash mismatch:\n  registry: %s\n  manifest: %s", hash, manifestHash)
	}

	// Test 4: Verify grader can be retrieved
	grader, err := registry.Get(hash)
	if err != nil {
		t.Fatalf("Failed to get grader: %v", err)
	}

	// Verify the expanded grader structure
	if grader.Kind != "Grader" {
		t.Errorf("expected kind 'Grader', got: %s", grader.Kind)
	}

	if grader.Metadata.Name != "csv-analysis-grader" {
		t.Errorf("expected name 'csv-analysis-grader', got: %s", grader.Metadata.Name)
	}

	if grader.Spec.Type != GraderTypeDeterministic {
		t.Errorf("expected type deterministic, got: %s", grader.Spec.Type)
	}

	if grader.Spec.Method != MethodJSONSchema {
		t.Errorf("expected method json_schema, got: %s", grader.Spec.Method)
	}

	t.Logf("✅ Grader retrieved successfully: %s (%s/%s)", grader.Metadata.Name, grader.Spec.Type, grader.Spec.Method)
}
