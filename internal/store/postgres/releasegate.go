package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/releasegate"
	"github.com/Lin-xun1113/SkillGate/internal/strategy"
	"github.com/jackc/pgx/v5"
)

// SaveSnapshot inserts the frozen metric snapshot once for an experiment.
func (s *Store) SaveSnapshot(ctx context.Context, snap releasegate.Snapshot) error {
	payload, err := json.Marshal(snap.Canonical())
	if err != nil {
		return wrapDatabaseError("marshal snapshot", err)
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO metrics_snapshots (snapshot_id, experiment_id, snapshot_hash, payload) VALUES ($1,$2,$3,$4) ON CONFLICT (experiment_id) DO NOTHING`, snap.Hash, snap.ExperimentID, snap.Hash, payload)
	if err != nil {
		return wrapDatabaseError("save snapshot", err)
	}
	return nil
}

// GetSnapshot loads the persisted frozen snapshot for an experiment.
func (s *Store) GetSnapshot(ctx context.Context, experimentID string) (*releasegate.Snapshot, error) {
	var hash string
	var payload []byte
	err := s.pool.QueryRow(ctx, `SELECT snapshot_hash, payload FROM metrics_snapshots WHERE experiment_id=$1`, experimentID).Scan(&hash, &payload)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("snapshot not found: %s", experimentID)
		}
		return nil, wrapDatabaseError("get snapshot", err)
	}
	var snap releasegate.Snapshot
	if err := json.Unmarshal(payload, &snap); err != nil {
		return nil, wrapDatabaseError("decode snapshot", err)
	}
	if snap.ExperimentID != "" && snap.ExperimentID != experimentID {
		return nil, fmt.Errorf("snapshot identity mismatch: %s", experimentID)
	}
	snap.ExperimentID = experimentID
	snap.Hash = hash
	return &snap, nil
}

// SaveDecision upserts the release decision for an experiment.
func (s *Store) SaveDecision(ctx context.Context, decision releasegate.Decision) error {
	trace, err := json.Marshal(map[string]any{"matched_rules": decision.MatchedRules, "evaluated_rules": decision.EvaluatedRules, "failed_conditions": decision.FailedConditions, "hard_gate_override": decision.HardGateOverride})
	if err != nil {
		return wrapDatabaseError("marshal decision trace", err)
	}
	createdAt := decision.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO release_decisions (decision_id,experiment_id,result,policy_id,policy_version,policy_hash,snapshot_hash,explanation,trace,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (experiment_id) DO UPDATE SET decision_id=EXCLUDED.decision_id,result=EXCLUDED.result,policy_id=EXCLUDED.policy_id,policy_version=EXCLUDED.policy_version,policy_hash=EXCLUDED.policy_hash,snapshot_hash=EXCLUDED.snapshot_hash,explanation=EXCLUDED.explanation,trace=EXCLUDED.trace,created_at=EXCLUDED.created_at`, decision.DecisionID, decision.ExperimentID, string(decision.Result), decision.PolicyID, decision.PolicyVersion, decision.PolicyHash, decision.SnapshotHash, decision.Explanation, trace, createdAt)
	if err != nil {
		return wrapDatabaseError("save decision", err)
	}
	return nil
}

// GetDecision loads the persisted decision for an experiment.
func (s *Store) GetDecision(ctx context.Context, experimentID string) (*releasegate.Decision, error) {
	var d releasegate.Decision
	var trace []byte
	err := s.pool.QueryRow(ctx, `SELECT decision_id,experiment_id,result,policy_id,policy_version,policy_hash,snapshot_hash,explanation,trace,created_at FROM release_decisions WHERE experiment_id=$1`, experimentID).Scan(&d.DecisionID, &d.ExperimentID, &d.Result, &d.PolicyID, &d.PolicyVersion, &d.PolicyHash, &d.SnapshotHash, &d.Explanation, &trace, &d.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("decision not found: %s", experimentID)
		}
		return nil, wrapDatabaseError("get decision", err)
	}
	var payload struct {
		MatchedRules     []string                   `json:"matched_rules"`
		EvaluatedRules   []strategy.EvaluatedRule   `json:"evaluated_rules"`
		FailedConditions []strategy.FailedCondition `json:"failed_conditions"`
		HardGateOverride *string                    `json:"hard_gate_override"`
	}
	if len(trace) > 0 {
		if err := json.Unmarshal(trace, &payload); err != nil {
			return nil, wrapDatabaseError("decode decision trace", err)
		}
		d.MatchedRules = payload.MatchedRules
		d.EvaluatedRules = payload.EvaluatedRules
		d.FailedConditions = payload.FailedConditions
		d.HardGateOverride = payload.HardGateOverride
	}
	return &d, nil
}
