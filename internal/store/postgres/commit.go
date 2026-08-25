package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
	leasetoken "github.com/Lin-xun1113/SkillGate/internal/lease"
	"github.com/Lin-xun1113/SkillGate/internal/retry"
	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
	"github.com/jackc/pgx/v5"
)

func (s *Store) Complete(ctx context.Context, completion scheduler.Completion) (scheduler.CommitResult, error) {
	manifestHash, err := canonicalManifestHash(completion.Manifest)
	if err != nil || manifestHash != completion.ManifestHash {
		return scheduler.CommitResult{}, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "result manifest hash 无法复算", Cause: err}
	}
	if completion.LogicalTrialID == "" || completion.TrialID == "" || completion.WorkerID == "" || completion.LeaseToken == "" || completion.LeaseGeneration < 1 || !scheduler.ValidOutcome(completion.Outcome) {
		return scheduler.CommitResult{}, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "completion 字段不完整"}
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return scheduler.CommitResult{}, wrapDatabaseError("开始 result commit 事务", err)
	}
	defer tx.Rollback(ctx)

	experimentID, err := experimentIDForTrial(ctx, tx, completion.TrialID)
	if err != nil {
		return scheduler.CommitResult{}, err
	}
	if err := lockExperimentShared(ctx, tx, experimentID); err != nil {
		return scheduler.CommitResult{}, err
	}
	row, err := lockCommitAttempt(ctx, tx, completion.TrialID)
	if err != nil {
		return scheduler.CommitResult{}, err
	}
	if row.LogicalTrialID != completion.LogicalTrialID {
		return scheduler.CommitResult{}, &scheduler.Error{Code: scheduler.CodeIdentityConflict, Message: "logical trial identity 不匹配"}
	}

	if !row.LeaseValid && row.OutcomeManifestHash != "" &&
		(row.OutcomeManifestHash == expiryManifestHash || row.OutcomeManifestHash == cancellationManifestHash || row.OutcomeManifestHash == budgetManifestHash) {
		return scheduler.CommitResult{}, &scheduler.Error{Code: scheduler.CodeLeaseExpired, Message: "lease 已过期"}
	}
	if row.Owner != completion.WorkerID {
		return scheduler.CommitResult{}, &scheduler.Error{Code: scheduler.CodeOwnerMismatch, Message: "lease owner 不匹配"}
	}
	if row.Generation != completion.LeaseGeneration || !leasetoken.Matches(row.TokenHash, completion.LeaseToken) {
		return scheduler.CommitResult{}, &scheduler.Error{Code: scheduler.CodeLeaseMismatch, Message: "lease token 或 fence 不匹配"}
	}
	if row.OutcomeManifestHash != "" {
		if row.OutcomeManifestHash != completion.ManifestHash {
			if !row.LeaseValid {
				return scheduler.CommitResult{}, &scheduler.Error{Code: scheduler.CodeLeaseExpired, Message: "lease 已过期"}
			}
			return scheduler.CommitResult{}, &scheduler.Error{Code: scheduler.CodeResultConflict, Message: "同一 attempt 已保存不同 result hash"}
		}
		if attemptStatusForOutcome(completion.Outcome) != row.AttemptStatus {
			return scheduler.CommitResult{}, &scheduler.Error{Code: scheduler.CodeResultConflict, Message: "同一 attempt 的 outcome 不一致"}
		}
		result, err := existingCommitResult(ctx, tx, row, completion.ManifestHash)
		if err != nil {
			return scheduler.CommitResult{}, err
		}
		result.Idempotent = true
		if err := tx.Commit(ctx); err != nil {
			return scheduler.CommitResult{}, wrapDatabaseError("提交幂等 result 事务", err)
		}
		return result, nil
	}
	if row.FinalResultID != "" {
		return scheduler.CommitResult{}, &scheduler.Error{Code: scheduler.CodeLogicalTrialTerminal, Message: "logical trial 已由其他 attempt 终结"}
	}
	if !row.LeaseValid {
		return scheduler.CommitResult{}, &scheduler.Error{Code: scheduler.CodeLeaseExpired, Message: "lease 已过期"}
	}
	if row.AttemptStatus != scheduler.AttemptLeased && row.AttemptStatus != scheduler.AttemptRunning {
		return scheduler.CommitResult{}, &scheduler.Error{Code: scheduler.CodeStatusConflict, Message: "attempt 当前不能 complete"}
	}
	if completion.EventSequence < row.EventSequence {
		return scheduler.CommitResult{}, &scheduler.Error{Code: scheduler.CodeStatusConflict, Message: "final event sequence 不能回退"}
	}
	if row.ExperimentStatus == scheduler.ExperimentCancelRequested && completion.Outcome != scheduler.OutcomeCancelled {
		return scheduler.CommitResult{}, &scheduler.Error{Code: scheduler.CodeExperimentCancelRequested, Message: "experiment 已请求取消，仅接受 CANCELLED outcome"}
	}

	attemptStatus := attemptStatusForOutcome(completion.Outcome)
	if _, err := tx.Exec(ctx, `
UPDATE trial_attempts
SET status=$2, event_sequence=$3, failure_category=NULLIF($4,''),
    outcome_manifest_hash=$5, outcome_manifest=$6, updated_at=clock_timestamp()
WHERE trial_id=$1`, completion.TrialID, attemptStatus, completion.EventSequence, string(completion.Category), completion.ManifestHash, completion.Manifest); err != nil {
		return scheduler.CommitResult{}, wrapDatabaseError("保存 attempt outcome", err)
	}

	policy := retry.Policy{MaxAttempts: row.MaxAttempts, Base: row.BackoffBase, Cap: row.BackoffCap, Retryable: categorySet(row.Retryable)}
	if completion.Outcome != scheduler.OutcomeSucceeded && completion.Outcome != scheduler.OutcomeCancelled && policy.Allows(completion.Category, row.AttemptNo) && row.ExperimentStatus != scheduler.ExperimentCancelRequested {
		nextTrialID, err := scheduleRetry(ctx, tx, row, completion.Category, s.jitter)
		if err != nil {
			return scheduler.CommitResult{}, err
		}
		if err := insertTransition(ctx, tx, row.ExperimentID, row.LogicalTrialID, completion.TrialID, completion.WorkerID, string(row.TrialStatus), string(scheduler.TrialRetryWait), "RETRY_SCHEDULED"); err != nil {
			return scheduler.CommitResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return scheduler.CommitResult{}, wrapDatabaseError("提交 retry result 事务", err)
		}
		return scheduler.CommitResult{LogicalTrialID: row.LogicalTrialID, TrialID: completion.TrialID, ManifestHash: completion.ManifestHash, Outcome: completion.Outcome, RetryScheduled: true, NextTrialID: nextTrialID}, nil
	}

	result, err := finalizeLogicalTrial(ctx, tx, row.ExperimentID, row.LogicalTrialID, completion.TrialID, completion.ManifestHash, completion.Manifest, completion.Outcome, completion.WorkerID, row.TrialStatus)
	if err != nil {
		return scheduler.CommitResult{}, err
	}
	if err := maybeFinalizeExperiment(ctx, tx, row.ExperimentID); err != nil {
		return scheduler.CommitResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return scheduler.CommitResult{}, wrapDatabaseError("提交 final result 事务", err)
	}
	return result, nil
}

type commitAttempt struct {
	lockedAttempt
	TrialStatus         scheduler.TrialStatus
	PairID              string
	Arm                 string
	AttemptNo           int
	MaxAttempts         int
	BackoffBase         time.Duration
	BackoffCap          time.Duration
	Retryable           []string
	OutcomeManifestHash string
	FinalResultID       string
}

func lockCommitAttempt(ctx context.Context, tx pgx.Tx, trialID string) (commitAttempt, error) {
	var row commitAttempt
	var backoffBaseMS, backoffCapMS int64
	err := tx.QueryRow(ctx, `
SELECT a.logical_trial_id, l.experiment_id, a.status, COALESCE(a.lease_owner,''), a.lease_token_hash,
       COALESCE(a.lease_generation,0), a.event_sequence,
       (a.lease_expires_at IS NOT NULL AND clock_timestamp() < a.lease_expires_at), e.status,
       l.status, l.pair_id, l.arm, a.attempt_no, l.max_attempts, l.backoff_base_ms, l.backoff_cap_ms,
       l.retryable_categories, COALESCE(a.outcome_manifest_hash,''), COALESCE(l.final_result_id,'')
FROM trial_attempts a
JOIN logical_trials l ON l.logical_trial_id=a.logical_trial_id
JOIN experiments e ON e.experiment_id=l.experiment_id
WHERE a.trial_id=$1
FOR UPDATE OF a, l`, trialID).Scan(
		&row.LogicalTrialID, &row.ExperimentID, &row.AttemptStatus, &row.Owner, &row.TokenHash,
		&row.Generation, &row.EventSequence, &row.LeaseValid, &row.ExperimentStatus,
		&row.TrialStatus, &row.PairID, &row.Arm, &row.AttemptNo, &row.MaxAttempts, &backoffBaseMS, &backoffCapMS,
		&row.Retryable, &row.OutcomeManifestHash, &row.FinalResultID,
	)
	if err == pgx.ErrNoRows {
		return commitAttempt{}, &scheduler.Error{Code: scheduler.CodeTrialNotFound, Message: "attempt 不存在"}
	}
	if err != nil {
		return commitAttempt{}, wrapDatabaseError("锁定 result attempt", err)
	}
	row.BackoffBase = time.Duration(backoffBaseMS) * time.Millisecond
	row.BackoffCap = time.Duration(backoffCapMS) * time.Millisecond
	return row, nil
}

func existingCommitResult(ctx context.Context, tx pgx.Tx, row commitAttempt, manifestHash string) (scheduler.CommitResult, error) {
	var result scheduler.CommitResult
	result.LogicalTrialID = row.LogicalTrialID
	result.TrialID = ""
	result.ManifestHash = manifestHash
	var outcome scheduler.Outcome
	var storedManifestHash string
	err := tx.QueryRow(ctx, `SELECT result_id, trial_id, manifest_hash, outcome FROM trial_results WHERE logical_trial_id=$1`, row.LogicalTrialID).Scan(&result.ResultID, &result.TrialID, &storedManifestHash, &outcome)
	if err == nil {
		result.ManifestHash = storedManifestHash
		result.Outcome = outcome
		return result, nil
	}
	if err != pgx.ErrNoRows {
		return scheduler.CommitResult{}, wrapDatabaseError("读取既有 result", err)
	}
	var nextTrialID string
	err = tx.QueryRow(ctx, `SELECT trial_id FROM trial_attempts WHERE logical_trial_id=$1 AND attempt_no=$2+1`, row.LogicalTrialID, row.AttemptNo).Scan(&nextTrialID)
	if err != nil && err != pgx.ErrNoRows {
		return scheduler.CommitResult{}, wrapDatabaseError("读取 retry attempt", err)
	}
	result.TrialID = rowAttemptID(row)
	result.Outcome = outcomeFromAttempt(row.AttemptStatus)
	result.RetryScheduled = nextTrialID != ""
	result.NextTrialID = nextTrialID
	return result, nil
}

func scheduleRetry(ctx context.Context, tx pgx.Tx, row commitAttempt, category retry.Category, random func(time.Duration) time.Duration) (string, error) {
	nextAttempt := row.AttemptNo + 1
	nextTrialID, err := identity.TrialID(row.PairID, row.Arm, nextAttempt)
	if err != nil {
		return "", &scheduler.Error{Code: scheduler.CodeIdentityConflict, Message: "无法计算 retry trial ID", Cause: err}
	}
	policy := retry.Policy{MaxAttempts: row.MaxAttempts, Base: row.BackoffBase, Cap: row.BackoffCap, Retryable: categorySet(row.Retryable)}
	delay := policy.FullJitter(nextAttempt, random)
	if _, err := tx.Exec(ctx, `
INSERT INTO trial_attempts (trial_id, logical_trial_id, attempt_no, status, priority, not_before)
SELECT $1, logical_trial_id, $2, 'PENDING', priority, clock_timestamp()+($3 * interval '1 millisecond')
FROM logical_trials WHERE logical_trial_id=$4`, nextTrialID, nextAttempt, delay.Milliseconds(), row.LogicalTrialID); err != nil {
		return "", wrapDatabaseError("创建 retry attempt", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE logical_trials SET status='RETRY_WAIT', current_attempt=$2, updated_at=clock_timestamp() WHERE logical_trial_id=$1`, row.LogicalTrialID, nextAttempt); err != nil {
		return "", wrapDatabaseError("更新 retry logical trial", err)
	}
	return nextTrialID, nil
}

func finalizeLogicalTrial(ctx context.Context, tx pgx.Tx, experimentID, logicalTrialID, trialID, manifestHash string, manifest []byte, outcome scheduler.Outcome, actor string, from scheduler.TrialStatus) (scheduler.CommitResult, error) {
	idempotencyKey, err := scheduler.ResultIdempotencyKey(trialID, manifestHash)
	if err != nil {
		return scheduler.CommitResult{}, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "无法计算 result idempotency key", Cause: err}
	}
	resultID, err := identity.HashCanonical(map[string]any{"logical_trial_id": logicalTrialID, "trial_id": trialID, "result_manifest_hash": manifestHash})
	if err != nil {
		return scheduler.CommitResult{}, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "无法计算 result ID", Cause: err}
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO trial_results (result_id, logical_trial_id, trial_id, idempotency_key, manifest_hash, manifest, outcome)
VALUES ($1,$2,$3,$4,$5,$6,$7)`, resultID, logicalTrialID, trialID, idempotencyKey, manifestHash, manifest, outcome); err != nil {
		return scheduler.CommitResult{}, wrapDatabaseError("插入 logical result", err)
	}
	status := trialStatusForOutcome(outcome)
	if _, err := tx.Exec(ctx, `UPDATE logical_trials SET status=$2, final_result_id=$3, updated_at=clock_timestamp() WHERE logical_trial_id=$1`, logicalTrialID, status, resultID); err != nil {
		return scheduler.CommitResult{}, wrapDatabaseError("终结 logical trial", err)
	}
	counterColumn := counterColumnForOutcome(outcome)
	query := fmt.Sprintf(`UPDATE experiments SET terminal_count=terminal_count+1, %s=%s+1, updated_at=clock_timestamp() WHERE experiment_id=$1`, counterColumn, counterColumn)
	if _, err := tx.Exec(ctx, query, experimentID); err != nil {
		return scheduler.CommitResult{}, wrapDatabaseError("更新 experiment counter", err)
	}
	if err := insertTransition(ctx, tx, experimentID, logicalTrialID, trialID, actor, string(from), string(status), "RESULT_COMMITTED"); err != nil {
		return scheduler.CommitResult{}, err
	}
	return scheduler.CommitResult{LogicalTrialID: logicalTrialID, TrialID: trialID, ResultID: resultID, ManifestHash: manifestHash, Outcome: outcome}, nil
}

func maybeFinalizeExperiment(ctx context.Context, tx pgx.Tx, experimentID string) error {
	var status scheduler.ExperimentStatus
	var terminal, total int
	if err := tx.QueryRow(ctx, `SELECT status, terminal_count, total_logical_trials FROM experiments WHERE experiment_id=$1`, experimentID).Scan(&status, &terminal, &total); err != nil {
		return wrapDatabaseError("读取 experiment counter", err)
	}
	if terminal != total {
		return nil
	}
	target := scheduler.ExperimentGrading
	if status == scheduler.ExperimentCancelRequested {
		target = scheduler.ExperimentCancelled
	}
	if _, err := tx.Exec(ctx, `UPDATE experiments SET status=$2, updated_at=clock_timestamp() WHERE experiment_id=$1`, experimentID, target); err != nil {
		return wrapDatabaseError("终结 experiment", err)
	}
	return nil
}

func canonicalManifestHash(raw []byte) (string, error) {
	return scheduler.ResultManifestHash(raw)
}

func categorySet(values []string) map[retry.Category]struct{} {
	result := make(map[retry.Category]struct{}, len(values))
	for _, value := range values {
		result[retry.Category(value)] = struct{}{}
	}
	return result
}

func attemptStatusForOutcome(outcome scheduler.Outcome) scheduler.AttemptStatus {
	switch outcome {
	case scheduler.OutcomeSucceeded:
		return scheduler.AttemptSucceeded
	case scheduler.OutcomeFailed:
		return scheduler.AttemptFailed
	case scheduler.OutcomeTimedOut:
		return scheduler.AttemptTimedOut
	default:
		return scheduler.AttemptCancelled
	}
}

func trialStatusForOutcome(outcome scheduler.Outcome) scheduler.TrialStatus {
	switch outcome {
	case scheduler.OutcomeSucceeded:
		return scheduler.TrialSucceeded
	case scheduler.OutcomeFailed:
		return scheduler.TrialFailed
	case scheduler.OutcomeTimedOut:
		return scheduler.TrialTimedOut
	default:
		return scheduler.TrialCancelled
	}
}

func counterColumnForOutcome(outcome scheduler.Outcome) string {
	switch outcome {
	case scheduler.OutcomeSucceeded:
		return "succeeded_count"
	case scheduler.OutcomeFailed:
		return "failed_count"
	case scheduler.OutcomeTimedOut:
		return "timed_out_count"
	default:
		return "cancelled_count"
	}
}

func outcomeFromAttempt(status scheduler.AttemptStatus) scheduler.Outcome {
	switch status {
	case scheduler.AttemptSucceeded:
		return scheduler.OutcomeSucceeded
	case scheduler.AttemptFailed:
		return scheduler.OutcomeFailed
	case scheduler.AttemptTimedOut:
		return scheduler.OutcomeTimedOut
	default:
		return scheduler.OutcomeCancelled
	}
}

func rowAttemptID(row commitAttempt) string {
	trialID, _ := identity.TrialID(row.PairID, row.Arm, row.AttemptNo)
	return trialID
}
