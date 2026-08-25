package postgres

import (
	"context"

	"github.com/Lin-xun1113/SkillGate/internal/retry"
	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
	"github.com/jackc/pgx/v5"
)

type SweepResult = scheduler.SweepResult

func (s *Store) SweepExpired(ctx context.Context, limit int) (SweepResult, error) {
	if limit < 1 || limit > 1000 {
		return SweepResult{}, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "sweep limit 必须位于 1..1000"}
	}
	result := SweepResult{}
	for result.Processed < limit {
		processed, retried, terminal, cancelled, err := s.sweepOne(ctx)
		if err != nil {
			return result, err
		}
		if !processed {
			break
		}
		result.Processed++
		if retried {
			result.Retried++
		}
		if terminal {
			result.Terminal++
		}
		if cancelled {
			result.Cancelled++
		}
	}
	return result, nil
}

func (s *Store) sweepOne(ctx context.Context) (processed, retried, terminal, cancelled bool, err error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, false, false, false, wrapDatabaseError("开始 sweep 事务", err)
	}
	defer tx.Rollback(ctx)

	var trialID string
	err = tx.QueryRow(ctx, `
SELECT trial_id
FROM trial_attempts
WHERE status IN ('LEASED','RUNNING') AND lease_expires_at <= clock_timestamp()
ORDER BY lease_expires_at, trial_id
LIMIT 1`).Scan(&trialID)
	if err == pgx.ErrNoRows {
		return sweepBudgetPending(ctx, tx)
	}
	if err != nil {
		return false, false, false, false, wrapDatabaseError("选择过期 attempt", err)
	}
	experimentID, err := experimentIDForTrial(ctx, tx, trialID)
	if err != nil {
		return false, false, false, false, err
	}
	if err := lockExperimentShared(ctx, tx, experimentID); err != nil {
		return false, false, false, false, err
	}
	var lockedTrialID string
	err = tx.QueryRow(ctx, `
SELECT trial_id FROM trial_attempts
WHERE trial_id=$1 AND status IN ('LEASED','RUNNING') AND lease_expires_at <= clock_timestamp()
FOR UPDATE SKIP LOCKED`, trialID).Scan(&lockedTrialID)
	if err == pgx.ErrNoRows {
		if err := tx.Commit(ctx); err != nil {
			return false, false, false, false, wrapDatabaseError("提交竞争 sweep", err)
		}
		return true, false, false, false, nil
	}
	if err != nil {
		return false, false, false, false, wrapDatabaseError("锁定过期 attempt", err)
	}
	row, err := lockCommitAttempt(ctx, tx, lockedTrialID)
	if err != nil {
		return false, false, false, false, err
	}
	if row.LeaseValid || (row.AttemptStatus != scheduler.AttemptLeased && row.AttemptStatus != scheduler.AttemptRunning) {
		if err := tx.Commit(ctx); err != nil {
			return false, false, false, false, wrapDatabaseError("提交空 sweep 事务", err)
		}
		return true, false, false, false, nil
	}

	if row.ExperimentStatus == scheduler.ExperimentCancelRequested {
		if _, err := tx.Exec(ctx, `
UPDATE trial_attempts
SET status='CANCELLED', failure_category=NULL,
    outcome_manifest_hash=$2, outcome_manifest=$3, updated_at=clock_timestamp()
WHERE trial_id=$1`, trialID, cancellationManifestHash, cancellationManifestJSON); err != nil {
			return false, false, false, false, wrapDatabaseError("取消过期 attempt", err)
		}
		if _, err := finalizeLogicalTrial(ctx, tx, row.ExperimentID, row.LogicalTrialID, trialID, cancellationManifestHash, cancellationManifestJSON, scheduler.OutcomeCancelled, "expiry-sweeper", row.TrialStatus); err != nil {
			return false, false, false, false, err
		}
		if err := maybeFinalizeExperiment(ctx, tx, row.ExperimentID); err != nil {
			return false, false, false, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return false, false, false, false, wrapDatabaseError("提交取消 sweep", err)
		}
		return true, false, true, true, nil
	}

	if _, err := tx.Exec(ctx, `
UPDATE trial_attempts
SET status='TIMED_OUT', failure_category='WORKER_LOST',
    outcome_manifest_hash=COALESCE(outcome_manifest_hash, $2),
    outcome_manifest=COALESCE(outcome_manifest, $3), updated_at=clock_timestamp()
WHERE trial_id=$1`, trialID, expiryManifestHash, expiryManifestJSON); err != nil {
		return false, false, false, false, wrapDatabaseError("终结过期 attempt", err)
	}

	var budgetAvailable bool
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp() < budget_deadline_at FROM experiments WHERE experiment_id=$1`, row.ExperimentID).Scan(&budgetAvailable); err != nil {
		return false, false, false, false, wrapDatabaseError("检查 experiment budget", err)
	}
	if !budgetAvailable {
		if _, err := tx.Exec(ctx, `
UPDATE trial_attempts
SET status='FAILED', failure_category='BUDGET_EXHAUSTED',
    outcome_manifest_hash=$2, outcome_manifest=$3, updated_at=clock_timestamp()
WHERE trial_id=$1`, trialID, budgetManifestHash, budgetManifestJSON); err != nil {
			return false, false, false, false, wrapDatabaseError("终结预算耗尽 attempt", err)
		}
		if _, err := finalizeLogicalTrial(ctx, tx, row.ExperimentID, row.LogicalTrialID, trialID, budgetManifestHash, budgetManifestJSON, scheduler.OutcomeFailed, "expiry-sweeper", row.TrialStatus); err != nil {
			return false, false, false, false, err
		}
		if err := maybeFinalizeExperiment(ctx, tx, row.ExperimentID); err != nil {
			return false, false, false, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return false, false, false, false, wrapDatabaseError("提交预算耗尽 sweep", err)
		}
		return true, false, true, false, nil
	}

	policy := retry.Policy{MaxAttempts: row.MaxAttempts, Base: row.BackoffBase, Cap: row.BackoffCap, Retryable: categorySet(row.Retryable)}
	if policy.Allows(retry.WorkerLost, row.AttemptNo) {
		if _, err := scheduleRetry(ctx, tx, row, retry.WorkerLost, s.jitter); err != nil {
			return false, false, false, false, err
		}
		if err := insertTransition(ctx, tx, row.ExperimentID, row.LogicalTrialID, trialID, "expiry-sweeper", string(row.TrialStatus), string(scheduler.TrialRetryWait), "LEASE_EXPIRED_RETRY"); err != nil {
			return false, false, false, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return false, false, false, false, wrapDatabaseError("提交 retry sweep", err)
		}
		return true, true, false, false, nil
	}

	if _, err := finalizeLogicalTrial(ctx, tx, row.ExperimentID, row.LogicalTrialID, trialID, expiryManifestHash, expiryManifestJSON, scheduler.OutcomeTimedOut, "expiry-sweeper", row.TrialStatus); err != nil {
		return false, false, false, false, err
	}
	if err := maybeFinalizeExperiment(ctx, tx, row.ExperimentID); err != nil {
		return false, false, false, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, false, false, false, wrapDatabaseError("提交 terminal sweep", err)
	}
	return true, false, true, false, nil
}

func sweepBudgetPending(ctx context.Context, tx pgx.Tx) (processed, retried, terminal, cancelled bool, err error) {
	var trialID string
	err = tx.QueryRow(ctx, `
SELECT a.trial_id
FROM trial_attempts a
JOIN logical_trials l ON l.logical_trial_id=a.logical_trial_id
JOIN experiments e ON e.experiment_id=l.experiment_id
WHERE a.status='PENDING' AND l.status IN ('PENDING','RETRY_WAIT')
  AND e.status IN ('QUEUED','RUNNING') AND e.budget_deadline_at <= clock_timestamp()
ORDER BY e.budget_deadline_at, a.created_at, a.trial_id
LIMIT 1`).Scan(&trialID)
	if err == pgx.ErrNoRows {
		return false, false, false, false, nil
	}
	if err != nil {
		return false, false, false, false, wrapDatabaseError("选择预算耗尽 attempt", err)
	}
	experimentID, err := experimentIDForTrial(ctx, tx, trialID)
	if err != nil {
		return false, false, false, false, err
	}
	if err := lockExperimentShared(ctx, tx, experimentID); err != nil {
		return false, false, false, false, err
	}
	var lockedTrialID string
	err = tx.QueryRow(ctx, `SELECT trial_id FROM trial_attempts WHERE trial_id=$1 AND status='PENDING' FOR UPDATE SKIP LOCKED`, trialID).Scan(&lockedTrialID)
	if err == pgx.ErrNoRows {
		if err := tx.Commit(ctx); err != nil {
			return false, false, false, false, wrapDatabaseError("提交预算竞争 sweep", err)
		}
		return true, false, false, false, nil
	}
	if err != nil {
		return false, false, false, false, wrapDatabaseError("锁定预算耗尽 attempt", err)
	}
	row, err := lockCommitAttempt(ctx, tx, lockedTrialID)
	if err != nil {
		return false, false, false, false, err
	}
	if _, err := tx.Exec(ctx, `
UPDATE trial_attempts
SET status='FAILED', failure_category='BUDGET_EXHAUSTED',
    outcome_manifest_hash=$2, outcome_manifest=$3, updated_at=clock_timestamp()
WHERE trial_id=$1`, trialID, budgetManifestHash, budgetManifestJSON); err != nil {
		return false, false, false, false, wrapDatabaseError("终结未领取预算 attempt", err)
	}
	if _, err := finalizeLogicalTrial(ctx, tx, row.ExperimentID, row.LogicalTrialID, trialID, budgetManifestHash, budgetManifestJSON, scheduler.OutcomeFailed, "budget-sweeper", row.TrialStatus); err != nil {
		return false, false, false, false, err
	}
	if err := maybeFinalizeExperiment(ctx, tx, row.ExperimentID); err != nil {
		return false, false, false, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, false, false, false, wrapDatabaseError("提交未领取预算 sweep", err)
	}
	return true, false, true, false, nil
}

var (
	expiryManifestJSON       = []byte(`{"version":"skillgate.result.v1","outcome":"TIMED_OUT","reason":"LEASE_EXPIRED"}`)
	cancellationManifestJSON = []byte(`{"version":"skillgate.result.v1","outcome":"CANCELLED","reason":"EXPERIMENT_CANCEL_REQUESTED"}`)
	budgetManifestJSON       = []byte(`{"version":"skillgate.result.v1","outcome":"FAILED","reason":"BUDGET_EXHAUSTED"}`)
)

const (
	expiryManifestHash       = "sha256:dbe82b5f7c4e1aaab3f640ed7f8a82a6d69d8b6f60132ef3cf9dc5485721712b"
	cancellationManifestHash = "sha256:f0387d3ab91070603658090fe431b9775e551377a3ebdb671f46c4d39c457786"
	budgetManifestHash       = "sha256:7f8bae66c01a835ac7bb711377b1a1259526b2a877b0eaab55dba6d25d98f5a5"
)
