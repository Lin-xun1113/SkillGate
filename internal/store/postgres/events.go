package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"github.com/Lin-xun1113/SkillGate/internal/lease"
	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
	"github.com/jackc/pgx/v5"
)

// ReportEvent persists an event only after validating the owning lease. The
// attempt row is locked so sequence checks and the insert are atomic.
func (s *Store) ReportEvent(ctx context.Context, event scheduler.Event) (scheduler.EventResult, error) {
	if event.EventID == "" || event.TrialID == "" || event.WorkerID == "" || event.LeaseToken == "" || event.Sequence <= 0 || event.EventType == "" {
		return scheduler.EventResult{Status: "REJECTED", Message: "event identity, lease and sequence are required"}, nil
	}
	var payload any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return scheduler.EventResult{Status: "REJECTED", Message: "payload must be valid JSON"}, nil
	}
	expectedHash, err := identity.HashCanonical(payload)
	if err != nil || event.PayloadHash == "" || expectedHash != event.PayloadHash {
		return scheduler.EventResult{Status: "REJECTED", Message: "payload hash mismatch"}, nil
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return scheduler.EventResult{}, wrapDatabaseError("开始 event 事务", err)
	}
	defer tx.Rollback(ctx)

	experimentID, err := experimentIDForTrial(ctx, tx, event.TrialID)
	if err != nil {
		return scheduler.EventResult{}, err
	}
	if err := lockExperimentShared(ctx, tx, experimentID); err != nil {
		return scheduler.EventResult{}, err
	}
	row, err := lockAttempt(ctx, tx, event.TrialID)
	if err != nil {
		return scheduler.EventResult{}, err
	}
	if event.ExperimentID != "" && event.ExperimentID != row.ExperimentID || event.LogicalTrialID != "" && event.LogicalTrialID != row.LogicalTrialID {
		return scheduler.EventResult{Status: "REJECTED", HighestSequence: row.EventSequence, Message: "event trial identity mismatch"}, nil
	}
	if row.Owner != event.WorkerID {
		return scheduler.EventResult{Status: "REJECTED", HighestSequence: row.EventSequence, Message: string(scheduler.CodeOwnerMismatch)}, nil
	}
	if event.LeaseGeneration > 0 && row.Generation != event.LeaseGeneration || !lease.Matches(row.TokenHash, event.LeaseToken) {
		return scheduler.EventResult{Status: "REJECTED", HighestSequence: row.EventSequence, Message: string(scheduler.CodeLeaseMismatch)}, nil
	}
	if !row.LeaseValid {
		return scheduler.EventResult{Status: "REJECTED", HighestSequence: row.EventSequence, Message: string(scheduler.CodeLeaseExpired)}, nil
	}

	var existingTrial string
	var existingHash string
	var existingSequence int64
	err = tx.QueryRow(ctx, `SELECT trial_id, payload_hash, sequence FROM trial_events WHERE event_id=$1`, event.EventID).Scan(&existingTrial, &existingHash, &existingSequence)
	if err == nil {
		if existingTrial != event.TrialID || existingHash != event.PayloadHash {
			return scheduler.EventResult{Status: "REJECTED", HighestSequence: row.EventSequence, Message: "event_id conflicts with existing event"}, nil
		}
		if err := tx.Commit(ctx); err != nil {
			return scheduler.EventResult{}, wrapDatabaseError("提交 duplicate event 事务", err)
		}
		return scheduler.EventResult{Status: "DUPLICATE_IGNORED", HighestSequence: row.EventSequence, Message: "event already recorded"}, nil
	}
	if err != pgx.ErrNoRows {
		return scheduler.EventResult{}, wrapDatabaseError("读取既有 event", err)
	}

	if event.Sequence > row.EventSequence+1 {
		return scheduler.EventResult{Status: "SEQUENCE_GAP", HighestSequence: row.EventSequence, Message: fmt.Sprintf("sequence gap: expected %d, got %d", row.EventSequence+1, event.Sequence)}, nil
	}
	if event.Sequence <= row.EventSequence {
		var sequenceHash string
		err := tx.QueryRow(ctx, `SELECT payload_hash FROM trial_events WHERE trial_id=$1 AND sequence=$2`, event.TrialID, event.Sequence).Scan(&sequenceHash)
		if err == nil && sequenceHash == event.PayloadHash {
			return scheduler.EventResult{Status: "DUPLICATE_IGNORED", HighestSequence: row.EventSequence, Message: "sequence already recorded"}, nil
		}
		return scheduler.EventResult{Status: "REJECTED", HighestSequence: row.EventSequence, Message: "event sequence must increase monotonically"}, nil
	}

	occurredAt := event.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	attemptNo := event.AttemptNo
	if attemptNo < 1 {
		attemptNo = row.AttemptNo
	}
	_, err = tx.Exec(ctx, `
INSERT INTO trial_events (event_id, trial_id, logical_trial_id, experiment_id, attempt_no, worker_id, sequence, event_type, occurred_at, payload, payload_hash)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11)`, event.EventID, event.TrialID, row.LogicalTrialID, row.ExperimentID, attemptNo, event.WorkerID, event.Sequence, event.EventType, occurredAt, event.Payload, event.PayloadHash)
	if err != nil {
		return scheduler.EventResult{}, &scheduler.Error{Code: scheduler.CodeDatabaseUnavailable, Message: fmt.Sprintf("写入 event失败: %v", err), Cause: err}
	}
	if _, err := tx.Exec(ctx, `UPDATE trial_attempts SET event_sequence=$2, updated_at=clock_timestamp() WHERE trial_id=$1`, event.TrialID, event.Sequence); err != nil {
		return scheduler.EventResult{}, wrapDatabaseError("更新 event sequence", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return scheduler.EventResult{}, wrapDatabaseError("提交 event 事务", err)
	}
	return scheduler.EventResult{Status: "ACCEPTED", HighestSequence: event.Sequence, Message: "event accepted"}, nil
}
