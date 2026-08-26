package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Lin-xun1113/SkillGate/internal/grading"
	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
	"github.com/jackc/pgx/v5"
)

// GetExperimentsInGrading retrieves all experiments in GRADING status
func (s *Store) GetExperimentsInGrading(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT experiment_id
		FROM experiments
		WHERE status = $1
		ORDER BY updated_at ASC
	`, scheduler.ExperimentGrading)

	if err != nil {
		return nil, wrapDatabaseError("query grading experiments", err)
	}
	defer rows.Close()

	var experimentIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, wrapDatabaseError("scan experiment id", err)
		}
		experimentIDs = append(experimentIDs, id)
	}

	if err := rows.Err(); err != nil {
		return nil, wrapDatabaseError("iterate experiments", err)
	}

	return experimentIDs, nil
}

// GetExperimentByID retrieves an experiment by ID
func (s *Store) GetExperimentByID(ctx context.Context, experimentID string) (*grading.Experiment, error) {
	var exp grading.Experiment
	var graderHash, policyHash *string

	err := s.pool.QueryRow(ctx, `
		SELECT experiment_id, grader_hash, policy_hash, total_logical_trials
		FROM experiments
		WHERE experiment_id = $1
	`, experimentID).Scan(&exp.ExperimentID, &graderHash, &policyHash, &exp.TotalTrials)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("experiment not found: %s", experimentID)
		}
		return nil, wrapDatabaseError("get experiment", err)
	}

	if graderHash != nil {
		exp.GraderHash = *graderHash
	}
	if policyHash != nil {
		exp.PolicyHash = *policyHash
	}
	exp.ArtifactsDir = fmt.Sprintf("artifacts/%s", experimentID)

	return &exp, nil
}

// GetTrialResults retrieves all trial results for an experiment.
// Arm is the canonical "without_skill"/"with_skill" value stored on logical_trials
// (see internal/experiment/compiler.go); token/latency usage lives in the
// trial_results.usage jsonb blob (scheduler.ResourceUsage shape), not scalar columns.
func (s *Store) GetTrialResults(ctx context.Context, experimentID string) ([]grading.TrialResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			r.result_id,
			r.trial_id,
			r.logical_trial_id,
			COALESCE(lt.case_id, ''),
			lt.arm,
			lt.repetition_index,
			COALESCE(lt.model_hash, ''),
			COALESCE(lt.environment_hash, ''),
			COALESCE(lt.grader_hash, ''),
			COALESCE(lt.evaluation_mode, ''),
			COALESCE(lt.population, ''),
			COALESCE(lt.polarity, ''),
			COALESCE(r.grades, '{}'),
			COALESCE(r.usage, '{}')
		FROM trial_results r
		JOIN logical_trials lt ON r.logical_trial_id = lt.logical_trial_id
		WHERE lt.experiment_id = $1
		ORDER BY lt.case_id, lt.arm, lt.repetition_index
	`, experimentID)

	if err != nil {
		return nil, wrapDatabaseError("query trial results", err)
	}
	defer rows.Close()

	var results []grading.TrialResult
	for rows.Next() {
		var tr grading.TrialResult
		var grades []byte
		var usage []byte

		err := rows.Scan(
			&tr.ResultID,
			&tr.TrialID,
			&tr.LogicalTrialID,
			&tr.CaseID,
			&tr.Arm,
			&tr.RepetitionIndex,
			&tr.ModelHash,
			&tr.EnvironmentHash,
			&tr.GraderHash,
			&tr.EvaluationMode,
			&tr.Population,
			&tr.Polarity,
			&grades,
			&usage,
		)
		if err != nil {
			return nil, wrapDatabaseError("scan trial result", err)
		}

		tr.Grades = json.RawMessage(grades)
		tr.ArtifactsDir = fmt.Sprintf("artifacts/%s/%s", experimentID, tr.TrialID)

		var resourceUsage scheduler.ResourceUsage
		if len(usage) > 0 {
			if jsonErr := json.Unmarshal(usage, &resourceUsage); jsonErr == nil {
				tr.InputTokens = int(resourceUsage.InputTokens)
				tr.OutputTokens = int(resourceUsage.OutputTokens)
				tr.LatencyMS = int(resourceUsage.ElapsedMS)
			}
		}

		results = append(results, tr)
	}

	if err := rows.Err(); err != nil {
		return nil, wrapDatabaseError("iterate trial results", err)
	}

	return results, nil
}

// UpdateTrialGrades updates the grades field for a trial result
func (s *Store) UpdateTrialGrades(ctx context.Context, resultID string, grades json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE trial_results
		SET grades = $2, updated_at = clock_timestamp()
		WHERE result_id = $1
	`, resultID, grades)

	if err != nil {
		return wrapDatabaseError("update trial grades", err)
	}

	return nil
}

// TransitionExperimentStatus updates experiment status with optimistic locking
func (s *Store) TransitionExperimentStatus(ctx context.Context, experimentID string, from, to scheduler.ExperimentStatus) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE experiments
		SET status = $3, updated_at = clock_timestamp()
		WHERE experiment_id = $1 AND status = $2
	`, experimentID, from, to)

	if err != nil {
		return wrapDatabaseError("transition experiment status", err)
	}

	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("experiment %s not in expected status %s", experimentID, from)
	}

	return nil
}

// SaveReport saves a report to the database
func (s *Store) SaveReport(ctx context.Context, report *grading.ExperimentReport) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO experiment_reports (
			report_id,
			experiment_id,
			report_type,
			file_path,
			file_hash,
			valid_pairs,
			invalid_pairs,
			mean_lift,
			ci_lower,
			ci_upper,
			statistically_significant,
			created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (experiment_id) DO UPDATE SET
			report_id = EXCLUDED.report_id,
			report_type = EXCLUDED.report_type,
			file_path = EXCLUDED.file_path,
			file_hash = EXCLUDED.file_hash,
			valid_pairs = EXCLUDED.valid_pairs,
			invalid_pairs = EXCLUDED.invalid_pairs,
			mean_lift = EXCLUDED.mean_lift,
			ci_lower = EXCLUDED.ci_lower,
			ci_upper = EXCLUDED.ci_upper,
			statistically_significant = EXCLUDED.statistically_significant,
			created_at = EXCLUDED.created_at
	`, report.ReportID, report.ExperimentID, report.ReportType, report.FilePath, report.FileHash,
		report.ValidPairs, report.InvalidPairs, report.MeanLift, report.CILower,
		report.CIUpper, report.StatisticallySignificant, report.CreatedAt)

	if err != nil {
		return wrapDatabaseError("save report", err)
	}

	return nil
}
