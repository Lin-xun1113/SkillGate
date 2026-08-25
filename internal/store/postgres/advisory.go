package postgres

import (
	"context"

	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
	"github.com/jackc/pgx/v5"
)

func experimentIDForTrial(ctx context.Context, tx pgx.Tx, trialID string) (string, error) {
	var experimentID string
	err := tx.QueryRow(ctx, `
SELECT l.experiment_id
FROM trial_attempts a
JOIN logical_trials l ON l.logical_trial_id=a.logical_trial_id
WHERE a.trial_id=$1`, trialID).Scan(&experimentID)
	if err == pgx.ErrNoRows {
		return "", &scheduler.Error{Code: scheduler.CodeTrialNotFound, Message: "attempt 不存在"}
	}
	if err != nil {
		return "", wrapDatabaseError("读取 attempt experiment", err)
	}
	return experimentID, nil
}

func lockExperimentShared(ctx context.Context, tx pgx.Tx, experimentID string) error {
	return lockAdvisoryShared(ctx, tx, experimentID, "experiment")
}

func lockExperimentExclusive(ctx context.Context, tx pgx.Tx, experimentID string) error {
	return lockAdvisoryExclusive(ctx, tx, experimentID, "experiment")
}

func lockAdvisoryShared(ctx context.Context, tx pgx.Tx, key, subject string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(hashtextextended($1, 0))`, key); err != nil {
		return wrapDatabaseError("获取 "+subject+" 共享事务锁", err)
	}
	return nil
}

func lockAdvisoryExclusive(ctx context.Context, tx pgx.Tx, key, subject string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
		return wrapDatabaseError("获取 "+subject+" 独占事务锁", err)
	}
	return nil
}
