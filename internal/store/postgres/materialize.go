package postgres

import (
	"context"
	"fmt"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"github.com/Lin-xun1113/SkillGate/internal/manifest"
	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
	"github.com/jackc/pgx/v5"
)

type MaterializeResult = scheduler.MaterializeResult

func (s *Store) Materialize(ctx context.Context, compiled *manifest.CompiledExperiment, options scheduler.MaterializeOptions) (MaterializeResult, error) {
	if compiled == nil || compiled.ManifestHash == "" || len(compiled.Pairs) == 0 {
		return MaterializeResult{}, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "compiled experiment 不能为空"}
	}
	policy := options.Policy()
	if err := policy.Validate(); err != nil {
		return MaterializeResult{}, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: err.Error()}
	}
	if options.Timeout <= 0 || options.BudgetTimeout <= 0 {
		return MaterializeResult{}, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "timeout 和 budget timeout 必须大于 0"}
	}
	planHash, err := identity.HashCanonical(map[string]any{
		"compiled": compiled,
		"runtime": map[string]any{
			"priority": options.Priority, "max_attempts": options.MaxAttempts,
			"timeout_ms": options.Timeout.Milliseconds(), "budget_timeout_ms": options.BudgetTimeout.Milliseconds(),
			"backoff_base_ms": options.BackoffBase.Milliseconds(), "backoff_cap_ms": options.BackoffCap.Milliseconds(),
			"retryable": options.Retryable,
		},
	})
	if err != nil {
		return MaterializeResult{}, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "无法计算 plan hash", Cause: err}
	}
	experimentID, err := identity.HashCanonical(map[string]any{"manifest_hash": compiled.ManifestHash, "plan_hash": planHash})
	if err != nil {
		return MaterializeResult{}, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "无法计算 experiment id", Cause: err}
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return MaterializeResult{}, wrapDatabaseError("开始物化事务", err)
	}
	defer tx.Rollback(ctx)

	if err := lockAdvisoryExclusive(ctx, tx, compiled.ManifestHash, "manifest"); err != nil {
		return MaterializeResult{}, err
	}
	if err := lockExperimentExclusive(ctx, tx, experimentID); err != nil {
		return MaterializeResult{}, err
	}
	var existingPlanHash string
	err = tx.QueryRow(ctx, `SELECT plan_hash FROM experiments WHERE manifest_hash=$1 FOR UPDATE`, compiled.ManifestHash).Scan(&existingPlanHash)
	if err == nil {
		if existingPlanHash != planHash {
			return MaterializeResult{}, &scheduler.Error{Code: scheduler.CodeIdentityConflict, Message: "相同 manifest hash 已对应不同 plan"}
		}
		if err := tx.Commit(ctx); err != nil {
			return MaterializeResult{}, wrapDatabaseError("提交幂等物化事务", err)
		}
		return MaterializeResult{ExperimentID: experimentID, LogicalTrials: compiled.TrialCount, Attempts: compiled.TrialCount, Idempotent: true}, nil
	}
	if err != pgx.ErrNoRows {
		return MaterializeResult{}, wrapDatabaseError("检查既有 experiment", err)
	}

	categoryStrings := make([]string, len(options.Retryable))
	for i, category := range options.Retryable {
		categoryStrings[i] = string(category)
	}
	logicalCount := 0
	for _, pair := range compiled.Pairs {
		for _, trial := range pair.Trials {
			if trial.Attempt != 1 {
				return MaterializeResult{}, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "M2 只接受 M1 attempt=1 的初始 plan"}
			}
			expectedTrialID, hashErr := identity.TrialID(pair.PairID, trial.Arm, trial.Attempt)
			if hashErr != nil || expectedTrialID != trial.TrialID {
				return MaterializeResult{}, &scheduler.Error{Code: scheduler.CodeIdentityConflict, Message: "M1 trial identity 无法复算", Cause: hashErr}
			}
			logicalID, hashErr := scheduler.LogicalTrialID(pair.PairID, trial.Arm)
			if hashErr != nil {
				return MaterializeResult{}, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "无法计算 logical trial identity", Cause: hashErr}
			}
			if logicalCount == 0 {
				_, err = tx.Exec(ctx, `
INSERT INTO experiments (experiment_id, manifest_hash, plan_hash, status, total_logical_trials, budget_deadline_at, grader_hash, policy_hash)
VALUES ($1,$2,$3,'QUEUED',$4,clock_timestamp()+($5 * interval '1 millisecond'),$6,$7)`, experimentID, compiled.ManifestHash, planHash, compiled.TrialCount, options.BudgetTimeout.Milliseconds(), pair.Identity.GraderHash, compiled.PolicyHash)
				if err != nil {
					return MaterializeResult{}, wrapDatabaseError("插入 experiment", err)
				}
			}
			_, err = tx.Exec(ctx, `
INSERT INTO logical_trials (
 logical_trial_id, experiment_id, pair_id, arm, status, priority,
 max_attempts, timeout_ms, backoff_base_ms, backoff_cap_ms, retryable_categories,
 case_id, repetition_index, model_hash, environment_hash, grader_hash,
 evaluation_mode, population, polarity
) VALUES ($1,$2,$3,$4,'PENDING',$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
				logicalID, experimentID, pair.PairID, trial.Arm, options.Priority,
				options.MaxAttempts, options.Timeout.Milliseconds(), options.BackoffBase.Milliseconds(), options.BackoffCap.Milliseconds(), categoryStrings,
				pair.CaseID, pair.Repetition, pair.Identity.ModelHash, pair.Identity.EnvironmentHash, pair.Identity.GraderHash,
				pair.EvaluationMode, pair.Population, pair.Polarity)
			if err != nil {
				return MaterializeResult{}, wrapDatabaseError("插入 logical trial", err)
			}
			_, err = tx.Exec(ctx, `
INSERT INTO trial_attempts (trial_id, logical_trial_id, attempt_no, status, priority)
VALUES ($1,$2,1,'PENDING',$3)`, trial.TrialID, logicalID, options.Priority)
			if err != nil {
				return MaterializeResult{}, wrapDatabaseError("插入初始 attempt", err)
			}
			logicalCount++
		}
	}
	if logicalCount != compiled.TrialCount {
		return MaterializeResult{}, &scheduler.Error{Code: scheduler.CodeIdentityConflict, Message: fmt.Sprintf("plan trial count=%d，实际=%d", compiled.TrialCount, logicalCount)}
	}
	if err := tx.Commit(ctx); err != nil {
		return MaterializeResult{}, wrapDatabaseError("提交物化事务", err)
	}
	return MaterializeResult{ExperimentID: experimentID, LogicalTrials: logicalCount, Attempts: logicalCount}, nil
}
