package runner

import (
	"encoding/json"
	"fmt"
)

// ProjectExecution fills ExecutionSpec into the payload based on manifest data.
// Returns updated payload JSON and both hashes (request_hash remains M3-frozen).
func ProjectExecution(
	payloadJSON string,
	payloadHash string,
	manifestHash string,
	caseID string,
	arm string,
	caseInput map[string]any,
	skillHash string,
	model map[string]any,
	toolPolicy map[string]any,
	environment map[string]any,
) (updatedJSON string, requestHash string, executionHash string, err error) {
	// Parse existing payload
	var payload TrialRequestPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return "", "", "", fmt.Errorf("failed to parse payload: %w", err)
	}

	// Build execution spec
	execution := &ExecutionSpec{
		CaseID:      caseID,
		CaseInput:   caseInput,
		SkillHash:   skillHash,
		Model:       model,
		ToolPolicy:  toolPolicy,
		Environment: environment,
	}

	// Compute execution hash using canonical algorithm
	// Hash the exact execution object, including case_input. manifest_hash and
	// arm are already covered by the scheduling/request identity envelope.
	executionHash, err = ComputeExecutionHashForSpec(execution)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to compute execution hash: %w", err)
	}

	// Update payload with execution
	payload.Execution = execution
	payload.ExecutionHash = executionHash

	// Serialize updated payload
	updatedBytes, err := json.Marshal(payload)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to marshal updated payload: %w", err)
	}

	// Request hash remains the M3-frozen value (scheduling identity only)
	return string(updatedBytes), payloadHash, executionHash, nil
}
