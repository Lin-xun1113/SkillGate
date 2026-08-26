package grader

import (
	"os"
	"path/filepath"
	"testing"
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
