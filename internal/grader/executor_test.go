package grader

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCSVSuiteAssertionsValidateArtifactAndTrace(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	hash, err := registry.Register("../../evals/csv-analysis/grader.yaml")
	if err != nil {
		t.Fatalf("register csv grader: %v", err)
	}
	root := t.TempDir()
	trialRoot := filepath.Join(root, "exp-1", "trial-1")
	if err := os.MkdirAll(filepath.Join(trialRoot, "output"), 0o755); err != nil {
		t.Fatal(err)
	}
	copyFile := func(src, dst string) {
		t.Helper()
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	copyFile("../../evals/csv-analysis/expected/monthly-summary.json", filepath.Join(trialRoot, "output", "summary.json"))
	trace := map[string]any{"events": []any{map[string]any{"event_type": "filesystem.read", "target": "fixtures/sales.csv"}}}
	traceJSON, _ := json.Marshal(trace)
	if err := os.WriteFile(filepath.Join(trialRoot, "trace.json"), traceJSON, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := NewExecutor(registry, root).ExecuteTrial(context.Background(), hash, "exp-1", "trial-1", "csv-explicit-001")
	if err != nil {
		t.Fatalf("execute suite: %v", err)
	}
	if result.Status != "scored" || !result.Passed || result.Score != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.GraderHash != hash || result.GraderVersion != 1 || result.EvidenceHash == "" {
		t.Fatalf("missing grader/evidence identity: %+v", result)
	}
}

func TestExecutorRejectsArtifactPathTraversal(t *testing.T) {
	root := t.TempDir()
	registry := NewRegistry(t.TempDir())
	graderPath := filepath.Join(t.TempDir(), "grader.yaml")
	graderYAML := []byte("apiVersion: skillgate.dev/v1alpha1\nkind: Grader\nmetadata:\n  name: traversal\n  version: 1\nspec:\n  type: deterministic\n  method: file_exists\n  config:\n    path: ../outside.txt\n  scoring:\n    passed_score: 1\n    failed_score: 0\n")
	if err := os.WriteFile(graderPath, graderYAML, 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := registry.Register(graderPath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewExecutor(registry, root).Execute(context.Background(), hash, "exp", "trial")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "invalid" {
		t.Fatalf("expected invalid traversal result, got %+v", result)
	}
}
