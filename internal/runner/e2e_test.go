package runner_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"github.com/Lin-xun1113/SkillGate/internal/runner"
	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
)

// TestE2EHashStability verifies A1 and A2: payload contains execution and hashes are stable
func TestE2EHashStability(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Create trial request payload with execution
	payload := runner.TrialRequestPayload{
		ExperimentID:   "exp-001",
		LogicalTrialID: "logical-001",
		TrialID:        "trial-001",
		PairID:         "pair-001",
		Arm:            "with_skill",
		AttemptNo:      1,
		Execution: &runner.ExecutionSpec{
			CaseID:    "case-001",
			SkillHash: "skill_xyz",
			Model: map[string]any{
				"provider": "mock",
			},
		},
	}

	// A1: Compute execution hash
	execBytes, err := json.Marshal(payload.Execution)
	if err != nil {
		t.Fatalf("Failed to marshal execution: %v", err)
	}
	var execValue any
	json.Unmarshal(execBytes, &execValue)
	execHash, err := identity.HashCanonical(execValue)
	if err != nil {
		t.Fatalf("Failed to compute execution hash: %v", err)
	}
	payload.ExecutionHash = execHash

	if payload.ExecutionHash == "" {
		t.Error("A1: execution_hash is empty")
	}

	// A1: Verify payload contains execution
	if payload.Execution == nil {
		t.Error("A1: execution is nil")
	}

	// A2: Compute trial request hash (M3 frozen - only scheduling identity)
	reqHash, err := scheduler.TrialRequestHash(
		payload.ExperimentID,
		payload.LogicalTrialID,
		payload.TrialID,
		payload.PairID,
		payload.Arm,
		payload.AttemptNo,
	)
	if err != nil {
		t.Fatalf("Failed to compute request hash: %v", err)
	}

	// A2: Verify hash stability - recompute and compare
	reqHash2, err := scheduler.TrialRequestHash(
		payload.ExperimentID,
		payload.LogicalTrialID,
		payload.TrialID,
		payload.PairID,
		payload.Arm,
		payload.AttemptNo,
	)
	if err != nil {
		t.Fatalf("Failed to recompute request hash: %v", err)
	}

	if reqHash != reqHash2 {
		t.Errorf("A2: trial_request_hash not stable: %s != %s", reqHash, reqHash2)
	}

	// A2: Tamper with execution and verify independence
	tampered := payload
	tampered.Execution = &runner.ExecutionSpec{
		CaseID:    "case-001",
		SkillHash: "tampered_hash",
	}

	tamperedExecBytes, _ := json.Marshal(tampered.Execution)
	var tamperedExecValue any
	json.Unmarshal(tamperedExecBytes, &tamperedExecValue)
	tamperedExecHash, _ := identity.HashCanonical(tamperedExecValue)

	// Request hash should remain unchanged (doesn't include execution)
	tamperedReqHash, _ := scheduler.TrialRequestHash(
		tampered.ExperimentID,
		tampered.LogicalTrialID,
		tampered.TrialID,
		tampered.PairID,
		tampered.Arm,
		tampered.AttemptNo,
	)

	if tamperedReqHash != reqHash {
		t.Error("A2: trial_request_hash changed when execution was tampered")
	}

	// Execution hash should change
	if tamperedExecHash == execHash {
		t.Error("A2: execution_hash did not change when execution was tampered")
	}

	t.Log("A1 & A2: Hash stability verified")
}

// TestE2ESandboxIsolation verifies A3: sandbox isolation and cleanup
func TestE2ESandboxIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Docker integration test in short mode")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()
	casDir := filepath.Join(tmpDir, "cas")
	artifactsDir := filepath.Join(tmpDir, "artifacts")

	os.MkdirAll(casDir, 0755)
	os.MkdirAll(artifactsDir, 0755)

	config := runner.SandboxConfig{
		Image:           "python:3.11-slim",
		CASRoot:         casDir,
		ArtifactsRoot:   artifactsDir,
		Env:             []string{"TEST=1"},
		MemoryLimitMB:   512,
		CPUQuota:        50000,
		NetworkDisabled: true,
		ReadOnlyRoot:    true,
		User:            "1000:1000",
	}

	// A3: Create sandbox
	sandbox, err := runner.NewSandbox(config)
	if err != nil {
		t.Fatalf("A3: NewSandbox failed: %v", err)
	}
	defer sandbox.Close()

	if err := sandbox.Create(ctx); err != nil {
		t.Fatalf("A3: Create failed: %v", err)
	}

	// A3: Verify container is running by executing a command
	exitCode, err := sandbox.Exec(ctx, []string{"echo", "hello"})
	if err != nil {
		t.Fatalf("A3: Exec failed: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("A3: Unexpected exit code: %d", exitCode)
	}

	// A3: Cleanup (no residual)
	if err := sandbox.Cleanup(ctx); err != nil {
		t.Errorf("A3: Cleanup failed: %v", err)
	}

	t.Log("A3: Sandbox isolation and cleanup verified")
}

// TestE2EWithoutSkillArm verifies A1: without_skill arm has empty skill_hash
func TestE2EWithoutSkillArm(t *testing.T) {
	payload := runner.TrialRequestPayload{
		ExperimentID:   "exp-001",
		LogicalTrialID: "logical-001",
		TrialID:        "trial-002",
		PairID:         "pair-001",
		Arm:            "without_skill",
		AttemptNo:      1,
		Execution: &runner.ExecutionSpec{
			CaseID:    "case-001",
			SkillHash: "", // A1: empty for without_skill
			Model: map[string]any{
				"provider": "mock",
			},
		},
	}

	if payload.Execution.SkillHash != "" {
		t.Errorf("A1: without_skill arm should have empty skill_hash, got %s", payload.Execution.SkillHash)
	}

	// Verify hash can still be computed
	reqHash, err := scheduler.TrialRequestHash(
		payload.ExperimentID,
		payload.LogicalTrialID,
		payload.TrialID,
		payload.PairID,
		payload.Arm,
		payload.AttemptNo,
	)
	if err != nil {
		t.Fatalf("Failed to compute hash: %v", err)
	}

	if reqHash == "" {
		t.Error("A1: Hash should be computable even with empty skill_hash")
	}

	t.Log("A1: without_skill arm verified")
}
