package postgres

import (
	"context"
	"encoding/json"
	"time"

	leasetoken "github.com/Lin-xun1113/SkillGate/internal/lease"
	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
	"github.com/jackc/pgx/v5"
)

func (s *Store) Claim(ctx context.Context, workerID string, leaseDuration time.Duration) (scheduler.Claim, error) {
	return s.ClaimFiltered(ctx, workerID, leaseDuration, scheduler.ClaimFilter{})
}

func (s *Store) ClaimFiltered(ctx context.Context, workerID string, leaseDuration time.Duration, filter scheduler.ClaimFilter) (scheduler.Claim, error) {
	if workerID == "" || leaseDuration <= 0 {
		return scheduler.Claim{}, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "worker ID 和 lease duration 必须有效"}
	}
	token, err := leasetoken.Generate()
	if err != nil {
		return scheduler.Claim{}, wrapDatabaseError("生成 lease token", err)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return scheduler.Claim{}, wrapDatabaseError("开始 claim 事务", err)
	}
	defer tx.Rollback(ctx)

	var claim scheduler.Claim
	var timeoutMS int64
	var budgetDeadline time.Time
	err = tx.QueryRow(ctx, `
SELECT e.experiment_id
FROM experiments e
WHERE e.status IN ('QUEUED','RUNNING') AND clock_timestamp() < e.budget_deadline_at
  AND ($1 = '' OR e.experiment_id = $1)
  AND EXISTS (
    SELECT 1 FROM logical_trials l
    JOIN trial_attempts a ON a.logical_trial_id=l.logical_trial_id
    WHERE l.experiment_id=e.experiment_id AND l.status IN ('PENDING','RETRY_WAIT')
      AND a.status='PENDING' AND a.not_before <= clock_timestamp()
  )
ORDER BY e.created_at, e.experiment_id
LIMIT 1`, filter.PreferredExperimentID).Scan(&claim.ExperimentID)
	if err == pgx.ErrNoRows {
		return scheduler.Claim{}, &scheduler.Error{Code: scheduler.CodeNotClaimable, Message: "当前没有可领取的 attempt"}
	}
	if err != nil {
		return scheduler.Claim{}, wrapDatabaseError("选择可领取 experiment", err)
	}
	if err := lockExperimentShared(ctx, tx, claim.ExperimentID); err != nil {
		return scheduler.Claim{}, err
	}
	err = tx.QueryRow(ctx, `
SELECT a.trial_id, a.logical_trial_id, a.attempt_no, l.timeout_ms, e.budget_deadline_at, l.pair_id, l.arm, e.manifest_hash
FROM trial_attempts a
JOIN logical_trials l ON l.logical_trial_id=a.logical_trial_id
JOIN experiments e ON e.experiment_id=l.experiment_id
WHERE e.experiment_id=$1 AND a.status='PENDING' AND a.not_before <= clock_timestamp()
  AND l.status IN ('PENDING','RETRY_WAIT')
  AND e.status IN ('QUEUED','RUNNING')
  AND clock_timestamp() < e.budget_deadline_at
ORDER BY a.priority DESC, a.not_before, a.created_at, a.trial_id
FOR UPDATE OF a SKIP LOCKED
	LIMIT 1`, claim.ExperimentID).Scan(&claim.TrialID, &claim.LogicalTrialID, &claim.Attempt, &timeoutMS, &budgetDeadline, &claim.PairID, &claim.Arm, &claim.ManifestHash)
	if err == pgx.ErrNoRows {
		return scheduler.Claim{}, &scheduler.Error{Code: scheduler.CodeNotClaimable, Message: "当前 experiment 的可领取 attempt 已被其他 Scheduler 获取"}
	}
	if err != nil {
		return scheduler.Claim{}, wrapDatabaseError("选择可领取 attempt", err)
	}
	var experimentStatus scheduler.ExperimentStatus
	if err := tx.QueryRow(ctx, `SELECT status FROM experiments WHERE experiment_id=$1`, claim.ExperimentID).Scan(&experimentStatus); err != nil {
		return scheduler.Claim{}, wrapDatabaseError("读取 experiment", err)
	}
	if experimentStatus != scheduler.ExperimentQueued && experimentStatus != scheduler.ExperimentRunning {
		return scheduler.Claim{}, &scheduler.Error{Code: scheduler.CodeNotClaimable, Message: "experiment 已停止领取"}
	}
	var logicalStatus scheduler.TrialStatus
	var generation int64
	if err := tx.QueryRow(ctx, `SELECT status, lease_generation FROM logical_trials WHERE logical_trial_id=$1 FOR UPDATE`, claim.LogicalTrialID).Scan(&logicalStatus, &generation); err != nil {
		return scheduler.Claim{}, wrapDatabaseError("锁定 logical trial", err)
	}
	if logicalStatus != scheduler.TrialPending && logicalStatus != scheduler.TrialRetryWait {
		return scheduler.Claim{}, &scheduler.Error{Code: scheduler.CodeNotClaimable, Message: "logical trial 当前不可领取"}
	}
	generation++
	claim.WorkerID = workerID
	claim.LeaseToken = token.Plaintext
	claim.LeaseGeneration = generation
	if err := tx.QueryRow(ctx, `
UPDATE trial_attempts
SET status='LEASED', lease_owner=$2, lease_token_hash=$3, lease_generation=$4,
    heartbeat_at=clock_timestamp(),
    deadline_at=LEAST(clock_timestamp()+($5 * interval '1 millisecond'), $7),
    lease_expires_at=LEAST(clock_timestamp()+($6 * interval '1 millisecond'), clock_timestamp()+($5 * interval '1 millisecond'), $7),
    updated_at=clock_timestamp()
WHERE trial_id=$1 AND status='PENDING'
RETURNING lease_expires_at, deadline_at`, claim.TrialID, workerID, token.Hash, generation, timeoutMS, leaseDuration.Milliseconds(), budgetDeadline).Scan(&claim.LeaseExpiresAt, &claim.Deadline); err != nil {
		return scheduler.Claim{}, wrapDatabaseError("更新 attempt lease", err)
	}
	requestHash, err := scheduler.TrialRequestHash(claim.ExperimentID, claim.LogicalTrialID, claim.TrialID, claim.PairID, claim.Arm, claim.Attempt)
	if err != nil {
		return scheduler.Claim{}, &scheduler.Error{Code: scheduler.CodeIdentityConflict, Message: "无法计算 trial request hash", Cause: err}
	}
	if _, err := tx.Exec(ctx, `UPDATE trial_attempts SET request_hash=$2 WHERE trial_id=$1`, claim.TrialID, requestHash); err != nil {
		return scheduler.Claim{}, wrapDatabaseError("保存 trial request hash", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE logical_trials SET status='LEASED', lease_generation=$2, updated_at=clock_timestamp() WHERE logical_trial_id=$1`, claim.LogicalTrialID, generation); err != nil {
		return scheduler.Claim{}, wrapDatabaseError("更新 logical trial lease", err)
	}
	if experimentStatus == scheduler.ExperimentQueued {
		if _, err := tx.Exec(ctx, `UPDATE experiments SET status='RUNNING', updated_at=clock_timestamp() WHERE experiment_id=$1`, claim.ExperimentID); err != nil {
			return scheduler.Claim{}, wrapDatabaseError("更新 experiment running", err)
		}
	}
	if err := insertTransition(ctx, tx, claim.ExperimentID, claim.LogicalTrialID, claim.TrialID, workerID, string(logicalStatus), string(scheduler.TrialLeased), "CLAIMED"); err != nil {
		return scheduler.Claim{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return scheduler.Claim{}, wrapDatabaseError("提交 claim 事务", err)
	}
	claim.LeaseExpiresAt = claim.LeaseExpiresAt.UTC()
	claim.Deadline = claim.Deadline.UTC()
	return claim, nil
}

func (s *Store) Start(ctx context.Context, heartbeat scheduler.Heartbeat) error {
	return s.updateLease(ctx, heartbeat, 0, true)
}

func (s *Store) Heartbeat(ctx context.Context, heartbeat scheduler.Heartbeat, leaseDuration time.Duration) error {
	if leaseDuration <= 0 {
		return &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "lease duration 必须大于 0"}
	}
	return s.updateLease(ctx, heartbeat, leaseDuration, false)
}

// HeartbeatWithResult returns the timestamp computed by PostgreSQL after the
// renewal. It is an optional extension of QueueStore for protocol callers.
func (s *Store) HeartbeatWithResult(ctx context.Context, heartbeat scheduler.Heartbeat, leaseDuration time.Duration) (scheduler.HeartbeatResult, error) {
	if err := s.Heartbeat(ctx, heartbeat, leaseDuration); err != nil {
		return scheduler.HeartbeatResult{}, err
	}
	var expires time.Time
	if err := s.pool.QueryRow(ctx, `SELECT lease_expires_at FROM trial_attempts WHERE trial_id=$1`, heartbeat.TrialID).Scan(&expires); err != nil {
		return scheduler.HeartbeatResult{}, wrapDatabaseError("读取 lease 到期时间", err)
	}
	return scheduler.HeartbeatResult{LeaseExpiresAt: expires.UTC()}, nil
}

func (s *Store) updateLease(ctx context.Context, heartbeat scheduler.Heartbeat, leaseDuration time.Duration, start bool) error {
	if heartbeat.TrialID == "" || heartbeat.WorkerID == "" || heartbeat.LeaseToken == "" || heartbeat.EventSequence < 0 {
		return &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "lease 请求字段不完整"}
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return wrapDatabaseError("开始 lease 更新事务", err)
	}
	defer tx.Rollback(ctx)

	experimentID, err := experimentIDForTrial(ctx, tx, heartbeat.TrialID)
	if err != nil {
		return err
	}
	if err := lockExperimentShared(ctx, tx, experimentID); err != nil {
		return err
	}
	row, err := lockAttempt(ctx, tx, heartbeat.TrialID)
	if err != nil {
		return err
	}
	if row.Owner != heartbeat.WorkerID {
		return &scheduler.Error{Code: scheduler.CodeOwnerMismatch, Message: "lease owner 不匹配"}
	}
	if (heartbeat.LeaseGeneration > 0 && row.Generation != heartbeat.LeaseGeneration) || !leasetoken.Matches(row.TokenHash, heartbeat.LeaseToken) {
		return &scheduler.Error{Code: scheduler.CodeLeaseMismatch, Message: "lease token 或 fence 不匹配"}
	}
	if !row.LeaseValid {
		return &scheduler.Error{Code: scheduler.CodeLeaseExpired, Message: "lease 已过期"}
	}
	if row.ExperimentStatus == scheduler.ExperimentCancelRequested {
		return &scheduler.Error{Code: scheduler.CodeExperimentCancelRequested, Message: "experiment 已请求取消"}
	}
	if heartbeat.EventSequence < row.EventSequence {
		return &scheduler.Error{Code: scheduler.CodeStatusConflict, Message: "event sequence 不能回退"}
	}
	usageJSON, err := json.Marshal(heartbeat.Usage)
	if err != nil {
		return &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "resource usage 无法序列化", Cause: err}
	}

	if start {
		if row.AttemptStatus == scheduler.AttemptRunning && heartbeat.EventSequence == row.EventSequence {
			return tx.Commit(ctx)
		}
		if row.AttemptStatus != scheduler.AttemptLeased {
			return &scheduler.Error{Code: scheduler.CodeStatusConflict, Message: "attempt 不是 LEASED"}
		}
		if _, err := tx.Exec(ctx, `
UPDATE trial_attempts SET status='RUNNING', started_at=COALESCE(started_at,clock_timestamp()), event_sequence=$2, phase=$3, usage=$4, updated_at=clock_timestamp()
WHERE trial_id=$1`, heartbeat.TrialID, heartbeat.EventSequence, nullablePhase(heartbeat.Phase), usageJSON); err != nil {
			return wrapDatabaseError("启动 attempt", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE logical_trials SET status='RUNNING', updated_at=clock_timestamp() WHERE logical_trial_id=$1`, row.LogicalTrialID); err != nil {
			return wrapDatabaseError("启动 logical trial", err)
		}
		if err := insertTransition(ctx, tx, row.ExperimentID, row.LogicalTrialID, heartbeat.TrialID, heartbeat.WorkerID, string(scheduler.TrialLeased), string(scheduler.TrialRunning), "STARTED"); err != nil {
			return err
		}
	} else {
		if row.AttemptStatus != scheduler.AttemptLeased && row.AttemptStatus != scheduler.AttemptRunning {
			return &scheduler.Error{Code: scheduler.CodeStatusConflict, Message: "attempt 不接受 heartbeat"}
		}
		if _, err := tx.Exec(ctx, `
UPDATE trial_attempts
SET heartbeat_at=clock_timestamp(),
    lease_expires_at=LEAST(clock_timestamp()+($2 * interval '1 millisecond'), deadline_at),
    event_sequence=$3, phase=$4, usage=$5, updated_at=clock_timestamp()
WHERE trial_id=$1`, heartbeat.TrialID, leaseDuration.Milliseconds(), heartbeat.EventSequence, nullablePhase(heartbeat.Phase), usageJSON); err != nil {
			return wrapDatabaseError("续租 attempt", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return wrapDatabaseError("提交 lease 更新事务", err)
	}
	return nil
}

type lockedAttempt struct {
	LogicalTrialID   string
	ExperimentID     string
	AttemptNo        int
	AttemptStatus    scheduler.AttemptStatus
	Owner            string
	TokenHash        []byte
	Generation       int64
	EventSequence    int64
	LeaseValid       bool
	ExperimentStatus scheduler.ExperimentStatus
}

func lockAttempt(ctx context.Context, tx pgx.Tx, trialID string) (lockedAttempt, error) {
	var row lockedAttempt
	err := tx.QueryRow(ctx, `
SELECT a.logical_trial_id, l.experiment_id, a.attempt_no, a.status, COALESCE(a.lease_owner,''), a.lease_token_hash,
       COALESCE(a.lease_generation,0), a.event_sequence,
       (a.lease_expires_at IS NOT NULL AND clock_timestamp() < a.lease_expires_at), e.status
FROM trial_attempts a
JOIN logical_trials l ON l.logical_trial_id=a.logical_trial_id
JOIN experiments e ON e.experiment_id=l.experiment_id
WHERE a.trial_id=$1
FOR UPDATE OF a, l`, trialID).Scan(
		&row.LogicalTrialID, &row.ExperimentID, &row.AttemptNo, &row.AttemptStatus, &row.Owner, &row.TokenHash,
		&row.Generation, &row.EventSequence, &row.LeaseValid, &row.ExperimentStatus,
	)
	if err == pgx.ErrNoRows {
		return lockedAttempt{}, &scheduler.Error{Code: scheduler.CodeTrialNotFound, Message: "attempt 不存在"}
	}
	if err != nil {
		return lockedAttempt{}, wrapDatabaseError("锁定 attempt", err)
	}
	return row, nil
}

func insertTransition(ctx context.Context, tx pgx.Tx, experimentID, logicalTrialID, trialID, actor, from, to, reason string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO trial_transition_events (experiment_id, logical_trial_id, trial_id, actor, from_status, to_status, reason_code)
VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7)`, experimentID, logicalTrialID, trialID, actor, from, to, reason)
	if err != nil {
		return wrapDatabaseError("写入 transition audit", err)
	}
	return nil
}

func nullablePhase(value string) any {
	if value == "" {
		return nil
	}
	return value
}
