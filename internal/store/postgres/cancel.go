package postgres

import (
	"context"

	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
	"github.com/jackc/pgx/v5"
)

type CancelResult = scheduler.CancelResult

func (s *Store) CancelExperiment(ctx context.Context, experimentID, actor, reason string) (CancelResult, error) {
	if experimentID == "" || actor == "" || reason == "" {
		return CancelResult{}, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "experiment ID、actor 和 reason 不能为空"}
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return CancelResult{}, wrapDatabaseError("开始 cancel 事务", err)
	}
	defer tx.Rollback(ctx)

	if err := lockExperimentExclusive(ctx, tx, experimentID); err != nil {
		return CancelResult{}, err
	}
	var status scheduler.ExperimentStatus
	if err := tx.QueryRow(ctx, `SELECT status FROM experiments WHERE experiment_id=$1 FOR UPDATE`, experimentID).Scan(&status); err == pgx.ErrNoRows {
		return CancelResult{}, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "experiment 不存在"}
	} else if err != nil {
		return CancelResult{}, wrapDatabaseError("锁定 experiment", err)
	}
	if status == scheduler.ExperimentCancelled || status == scheduler.ExperimentCancelRequested {
		if err := tx.Commit(ctx); err != nil {
			return CancelResult{}, wrapDatabaseError("提交幂等 cancel", err)
		}
		return CancelResult{ExperimentID: experimentID, Status: status, Idempotent: true}, nil
	}
	if status != scheduler.ExperimentQueued && status != scheduler.ExperimentRunning {
		return CancelResult{}, &scheduler.Error{Code: scheduler.CodeStatusConflict, Message: "experiment 当前不可取消"}
	}
	if _, err := tx.Exec(ctx, `
UPDATE experiments
SET status='CANCEL_REQUESTED', cancel_requested_at=clock_timestamp(), cancel_reason=$2, updated_at=clock_timestamp()
WHERE experiment_id=$1`, experimentID, reason); err != nil {
		return CancelResult{}, wrapDatabaseError("标记 experiment cancel requested", err)
	}

	rows, err := tx.Query(ctx, `
SELECT l.logical_trial_id, a.trial_id, l.status
FROM logical_trials l
JOIN trial_attempts a ON a.logical_trial_id=l.logical_trial_id AND a.attempt_no=l.current_attempt
WHERE l.experiment_id=$1 AND l.status IN ('PENDING','RETRY_WAIT')
ORDER BY l.logical_trial_id
FOR UPDATE OF l, a`, experimentID)
	if err != nil {
		return CancelResult{}, wrapDatabaseError("锁定待取消 trials", err)
	}
	type item struct {
		logicalTrialID string
		trialID        string
		from           scheduler.TrialStatus
	}
	items := []item{}
	for rows.Next() {
		var current item
		if err := rows.Scan(&current.logicalTrialID, &current.trialID, &current.from); err != nil {
			rows.Close()
			return CancelResult{}, wrapDatabaseError("读取待取消 trial", err)
		}
		items = append(items, current)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return CancelResult{}, wrapDatabaseError("遍历待取消 trials", err)
	}
	rows.Close()

	cancelManifest := []byte(`{"version":"skillgate.result.v1","outcome":"CANCELLED","reason":"EXPERIMENT_CANCEL_REQUESTED"}`)
	cancelHash, err := canonicalManifestHash(cancelManifest)
	if err != nil {
		return CancelResult{}, err
	}
	for _, current := range items {
		if _, err := tx.Exec(ctx, `
UPDATE trial_attempts
SET status='CANCELLED', cancel_requested_at=clock_timestamp(),
    outcome_manifest_hash=$2, outcome_manifest=$3, updated_at=clock_timestamp()
WHERE trial_id=$1`, current.trialID, cancelHash, cancelManifest); err != nil {
			return CancelResult{}, wrapDatabaseError("取消 pending attempt", err)
		}
		if _, err := finalizeLogicalTrial(ctx, tx, experimentID, current.logicalTrialID, current.trialID, cancelHash, cancelManifest, scheduler.OutcomeCancelled, actor, current.from); err != nil {
			return CancelResult{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
UPDATE trial_attempts a
SET cancel_requested_at=clock_timestamp(), updated_at=clock_timestamp()
FROM logical_trials l
WHERE a.logical_trial_id=l.logical_trial_id AND l.experiment_id=$1
  AND a.status IN ('LEASED','RUNNING')`, experimentID); err != nil {
		return CancelResult{}, wrapDatabaseError("传播运行中 cancel", err)
	}
	if err := maybeFinalizeExperiment(ctx, tx, experimentID); err != nil {
		return CancelResult{}, err
	}
	if err := tx.QueryRow(ctx, `SELECT status FROM experiments WHERE experiment_id=$1`, experimentID).Scan(&status); err != nil {
		return CancelResult{}, wrapDatabaseError("读取取消后状态", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return CancelResult{}, wrapDatabaseError("提交 cancel 事务", err)
	}
	return CancelResult{ExperimentID: experimentID, Status: status, Cancelled: len(items)}, nil
}
