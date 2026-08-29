package runner

import (
	"testing"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
)

func TestProtocolHashGoldenVector(t *testing.T) {
	request := map[string]any{
		"experiment_id":    "e",
		"logical_trial_id": "l",
		"trial_id":         "t",
		"pair_id":          "p",
		"arm":              "with_skill",
		"attempt_no":       1,
	}
	got, err := identity.HashCanonical(request)
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:d14e6c29d72ed21c2d5772682c22de089c39105cf6d6b5464a21cf90f08808eb"
	if got != want {
		t.Fatalf("request hash drift: got %s want %s", got, want)
	}

	execution := &ExecutionSpec{
		CaseID:      "case-1",
		CaseInput:   map[string]any{"prompt": "line1\r\nline2"},
		SkillHash:   "",
		Model:       map[string]any{"name": "mock", "provider": "fixture"},
		ToolPolicy:  map[string]any{"tools": []any{}},
		Environment: map[string]any{},
	}
	hash, err := ComputeExecutionHashForSpec(execution)
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != len("sha256:")+64 || hash[:7] != "sha256:" {
		t.Fatalf("execution hash must be prefixed sha256: got %q", hash)
	}
	if hash != "sha256:ccfb9d18e89268a813aef5cad7dd04586177890a74f2d65ae0517b8912082883" {
		t.Fatalf("execution hash drift: got %s", hash)
	}
	if hash2, _ := ComputeExecutionHashForSpec(execution); hash != hash2 {
		t.Fatal("execution hash is not deterministic")
	}
}

func TestExecutionHashUsesFullSpec(t *testing.T) {
	base := &ExecutionSpec{CaseID: "case", CaseInput: map[string]any{"x": 1}, Model: map[string]any{"provider": "mock"}, ToolPolicy: map[string]any{}, Environment: map[string]any{}}
	first, err := ComputeExecutionHashForSpec(base)
	if err != nil {
		t.Fatal(err)
	}
	base.CaseInput["x"] = 2
	second, err := ComputeExecutionHashForSpec(base)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("case_input mutation must change execution hash")
	}
}
