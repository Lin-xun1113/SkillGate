package runner

import (
	"encoding/json"
	"fmt"

	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
)

// ExecutionSpec contains what to execute (skill, case, model, policy).
type ExecutionSpec struct {
	CaseID      string         `json:"case_id"`
	CaseInput   map[string]any `json:"case_input"`
	SkillHash   string         `json:"skill_hash"` // empty for without_skill arm
	Model       map[string]any `json:"model"`
	ToolPolicy  map[string]any `json:"tool_policy"`
	Environment map[string]any `json:"environment"`
}

// TrialRequestPayload represents the deterministic payload delivered to a worker for a trial.
type TrialRequestPayload struct {
	ExperimentID   string         `json:"experiment_id"`
	LogicalTrialID string         `json:"logical_trial_id"`
	TrialID        string         `json:"trial_id"`
	PairID         string         `json:"pair_id"`
	Arm            string         `json:"arm"`
	AttemptNo      int            `json:"attempt_no"`
	Execution      *ExecutionSpec `json:"execution,omitempty"`
	ExecutionHash  string         `json:"execution_hash,omitempty"`
}

// BuildTrialRequestPayload constructs the canonical JSON and SHA-256 request hash for a claim.
// The request hash remains M3-frozen (only scheduling identity); execution is separate.
func BuildTrialRequestPayload(claim scheduler.Claim) (jsonStr string, hash string, err error) {
	if claim.ExperimentID == "" || claim.TrialID == "" {
		return "", "", fmt.Errorf("experiment_id and trial_id cannot be empty")
	}
	payload := TrialRequestPayload{
		ExperimentID:   claim.ExperimentID,
		LogicalTrialID: claim.LogicalTrialID,
		TrialID:        claim.TrialID,
		PairID:         claim.PairID,
		Arm:            claim.Arm,
		AttemptNo:      claim.Attempt,
		// Execution and ExecutionHash are populated by ProjectExecution
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal trial request payload: %w", err)
	}
	// TrialRequestHash unchanged: only scheduling identity (M3 frozen)
	reqHash, err := scheduler.TrialRequestHash(claim.ExperimentID, claim.LogicalTrialID, claim.TrialID, claim.PairID, claim.Arm, claim.Attempt)
	if err != nil {
		return "", "", fmt.Errorf("failed to compute request hash: %w", err)
	}
	return string(bytes), reqHash, nil
}
