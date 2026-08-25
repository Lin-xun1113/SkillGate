package runner

import (
	"encoding/json"
	"fmt"

	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
)

// TrialRequestPayload represents the deterministic payload delivered to a worker for a trial.
type TrialRequestPayload struct {
	ExperimentID   string `json:"experiment_id"`
	LogicalTrialID string `json:"logical_trial_id"`
	TrialID        string `json:"trial_id"`
	PairID         string `json:"pair_id"`
	Arm            string `json:"arm"`
	AttemptNo      int    `json:"attempt_no"`
}

// BuildTrialRequestPayload constructs the canonical JSON and SHA-256 request hash for a claim.
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
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal trial request payload: %w", err)
	}
	reqHash, err := scheduler.TrialRequestHash(claim.ExperimentID, claim.LogicalTrialID, claim.TrialID, claim.PairID, claim.Arm, claim.Attempt)
	if err != nil {
		return "", "", fmt.Errorf("failed to compute request hash: %w", err)
	}
	return string(bytes), reqHash, nil
}
