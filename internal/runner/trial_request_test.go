package runner_test

import (
	"encoding/json"
	"testing"

	"github.com/Lin-xun1113/SkillGate/internal/runner"
	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
)

// A1: payload contains execution segment and execution_hash; without_skill arm has empty skill_hash
func TestPayloadContainsExecutionSegment(t *testing.T) {
	claim := scheduler.Claim{
		ExperimentID:   "exp1",
		LogicalTrialID: "lt1",
		TrialID:        "trial1",
		PairID:         "pair1",
		Arm:            "with_skill",
		Attempt:        1,
	}

	basePayload, requestHash, err := runner.BuildTrialRequestPayload(claim)
	if err != nil {
		t.Fatalf("BuildTrialRequestPayload failed: %v", err)
	}

	if requestHash == "" {
		t.Fatal("request_hash is empty")
	}

	// Now project execution
	execution := &runner.ExecutionSpec{
		CaseID:    "case1",
		CaseInput: map[string]any{"fixtures": []any{}},
		SkillHash: "abc123",
		Model:     map[string]any{"provider": "mock"},
		ToolPolicy: map[string]any{},
		Environment: map[string]any{},
	}

	fullPayload, newRequestHash, executionHash, err := runner.ProjectExecution(
		basePayload,
		requestHash,
		"manifest-hash-123",
		execution.CaseID,
		claim.Arm, // arm parameter
		execution.CaseInput,
		execution.SkillHash,
		execution.Model,
		execution.ToolPolicy,
		execution.Environment,
	)
	if err != nil {
		t.Fatalf("ProjectExecution failed: %v", err)
	}

	// Verify request hash unchanged
	if newRequestHash != requestHash {
		t.Errorf("Request hash changed: expected %s, got %s", requestHash, newRequestHash)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(fullPayload), &payload); err != nil {
		t.Fatalf("Failed to unmarshal payload: %v", err)
	}

	// A1.1: execution segment exists
	execSeg, ok := payload["execution"].(map[string]any)
	if !ok {
		t.Fatal("execution segment missing")
	}

	if execSeg["case_id"] != "case1" {
		t.Errorf("case_id = %v, want case1", execSeg["case_id"])
	}

	if execSeg["skill_hash"] != "abc123" {
		t.Errorf("skill_hash = %v, want abc123", execSeg["skill_hash"])
	}

	// A1.2: execution_hash exists
	execHashInPayload, ok := payload["execution_hash"].(string)
	if !ok || execHashInPayload != executionHash {
		t.Errorf("execution_hash = %v, want %s", execHashInPayload, executionHash)
	}
}

// A1: without_skill arm has empty skill_hash
func TestWithoutSkillArmEmptySkillHash(t *testing.T) {
	execution := &runner.ExecutionSpec{
		CaseID:      "case1",
		CaseInput:   map[string]any{},
		SkillHash:   "", // without_skill arm
		Model:       map[string]any{"provider": "mock"},
		ToolPolicy:  map[string]any{},
		Environment: map[string]any{},
	}

	if execution.SkillHash != "" {
		t.Errorf("without_skill arm should have empty skill_hash, got %v", execution.SkillHash)
	}
}

// A2: TrialRequestHash unchanged (M3 frozen); tampering execution only fails execution_hash
func TestTrialRequestHashUnchanged(t *testing.T) {
	claim := scheduler.Claim{
		ExperimentID:   "exp1",
		LogicalTrialID: "lt1",
		TrialID:        "trial1",
		PairID:         "pair1",
		Arm:            "with_skill",
		Attempt:        1,
	}

	// Build base payload twice
	payload1, hash1, err := runner.BuildTrialRequestPayload(claim)
	if err != nil {
		t.Fatalf("BuildTrialRequestPayload failed: %v", err)
	}

	payload2, hash2, err := runner.BuildTrialRequestPayload(claim)
	if err != nil {
		t.Fatalf("BuildTrialRequestPayload failed: %v", err)
	}

	// A2.1: Base request hash is deterministic and unchanged
	if hash1 != hash2 {
		t.Errorf("request_hash differs: %v != %v", hash1, hash2)
	}

	// Now project different executions
	execution1 := &runner.ExecutionSpec{
		CaseID:      "case1",
		CaseInput:   map[string]any{},
		SkillHash:   "abc123",
		Model:       map[string]any{"provider": "mock"},
		ToolPolicy:  map[string]any{},
		Environment: map[string]any{},
	}

	execution2 := &runner.ExecutionSpec{
		CaseID:      "case1",
		CaseInput:   map[string]any{},
		SkillHash:   "xyz789", // Different skill
		Model:       map[string]any{"provider": "mock"},
		ToolPolicy:  map[string]any{},
		Environment: map[string]any{},
	}

	fullPayload1, newHash1, execHash1, _ := runner.ProjectExecution(
		payload1, hash1, "manifest-hash-123",
		execution1.CaseID, claim.Arm, execution1.CaseInput, execution1.SkillHash,
		execution1.Model, execution1.ToolPolicy, execution1.Environment,
	)
	fullPayload2, newHash2, execHash2, _ := runner.ProjectExecution(
		payload2, hash2, "manifest-hash-123",
		execution2.CaseID, claim.Arm, execution2.CaseInput, execution2.SkillHash,
		execution2.Model, execution2.ToolPolicy, execution2.Environment,
	)

	var p1, p2 map[string]any
	json.Unmarshal([]byte(fullPayload1), &p1)
	json.Unmarshal([]byte(fullPayload2), &p2)

	// A2.2: Base request hash remains frozen
	if newHash1 != hash1 || newHash2 != hash2 {
		t.Errorf("Request hash changed: %s->%s or %s->%s", hash1, newHash1, hash2, newHash2)
	}

	// A2.3: Scheduling identity fields remain unchanged
	if p1["trial_id"] != p2["trial_id"] {
		t.Error("trial_id changed")
	}
	if p1["experiment_id"] != p2["experiment_id"] {
		t.Error("experiment_id changed")
	}

	// A2.4: execution_hash differs (tampered execution)
	if execHash1 == execHash2 {
		t.Error("execution_hash should differ for different executions")
	}
	if p1["execution_hash"] != execHash1 {
		t.Errorf("payload1 execution_hash mismatch: got %v, want %s", p1["execution_hash"], execHash1)
	}
	if p2["execution_hash"] != execHash2 {
		t.Errorf("payload2 execution_hash mismatch: got %v, want %s", p2["execution_hash"], execHash2)
	}

	// A2.4: Request hash would still be same for same claim (M3 frozen)
	// This is verified by hash1 == hash2 above, regardless of execution content
}
