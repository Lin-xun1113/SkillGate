package runner

import (
	"encoding/json"
	"testing"
)

func TestProjectExecution(t *testing.T) {
	// Build base payload (M3 frozen)
	basePayload := TrialRequestPayload{
		ExperimentID:   "exp1",
		LogicalTrialID: "lt1",
		TrialID:        "t1",
		PairID:         "p1",
		Arm:            "with_skill",
		AttemptNo:      1,
	}
	baseBytes, _ := json.Marshal(basePayload)
	baseJSON := string(baseBytes)
	baseHash := "frozen-m3-hash"

	// Project execution
	updatedJSON, newRequestHash, executionHash, err := ProjectExecution(
		baseJSON,
		baseHash,
		"manifest-hash-123",
		"case1",
		"with_skill", // arm parameter
		map[string]any{"fixtures": []map[string]any{{"type": "text", "content": "test"}}},
		"skill-abc123",
		map[string]any{"provider": "mock", "name": "mock-model"},
		map[string]any{},
		map[string]any{},
	)

	if err != nil {
		t.Fatalf("ProjectExecution failed: %v", err)
	}

	// Verify request hash unchanged
	if newRequestHash != baseHash {
		t.Errorf("Request hash changed: expected %s, got %s", baseHash, newRequestHash)
	}

	// Verify execution hash is non-empty
	if executionHash == "" {
		t.Error("Execution hash is empty")
	}

	// Parse updated payload
	var updated TrialRequestPayload
	if err := json.Unmarshal([]byte(updatedJSON), &updated); err != nil {
		t.Fatalf("Failed to parse updated payload: %v", err)
	}

	// Verify execution segment present
	if updated.Execution == nil {
		t.Fatal("Execution segment missing")
	}

	if updated.Execution.CaseID != "case1" {
		t.Errorf("CaseID mismatch: got %s", updated.Execution.CaseID)
	}

	if updated.Execution.SkillHash != "skill-abc123" {
		t.Errorf("SkillHash mismatch: got %s", updated.Execution.SkillHash)
	}

	if updated.ExecutionHash != executionHash {
		t.Errorf("ExecutionHash mismatch in payload: got %s", updated.ExecutionHash)
	}

	t.Logf("✓ Projection successful: request_hash=%s (frozen), execution_hash=%s",
		newRequestHash[:8], executionHash[:8])
}

func TestProjectExecutionWithoutSkill(t *testing.T) {
	basePayload := TrialRequestPayload{
		ExperimentID:   "exp1",
		LogicalTrialID: "lt1",
		TrialID:        "t2",
		PairID:         "p1",
		Arm:            "without_skill",
		AttemptNo:      1,
	}
	baseBytes, _ := json.Marshal(basePayload)
	baseJSON := string(baseBytes)
	baseHash := "frozen-m3-hash"

	updatedJSON, newRequestHash, executionHash, err := ProjectExecution(
		baseJSON,
		baseHash,
		"manifest-hash-123",
		"case1",
		"without_skill", // arm parameter
		map[string]any{"fixtures": []map[string]any{{"type": "text", "content": "test"}}},
		"", // Empty skill_hash for without_skill arm
		map[string]any{"provider": "mock", "name": "mock-model"},
		map[string]any{},
		map[string]any{},
	)

	if err != nil {
		t.Fatalf("ProjectExecution failed: %v", err)
	}

	// Verify request hash unchanged
	if newRequestHash != baseHash {
		t.Errorf("Request hash changed: expected %s, got %s", baseHash, newRequestHash)
	}

	// Verify execution hash is non-empty even for without_skill
	if executionHash == "" {
		t.Error("Execution hash is empty")
	}

	var updated TrialRequestPayload
	json.Unmarshal([]byte(updatedJSON), &updated)

	if updated.Execution.SkillHash != "" {
		t.Errorf("without_skill arm should have empty skill_hash, got %s", updated.Execution.SkillHash)
	}

	t.Logf("✓ without_skill projection: skill_hash is empty as expected, execution_hash=%s", executionHash[:8])
}
