-- +goose Up
ALTER TABLE experiments
    ADD COLUMN IF NOT EXISTS grader_hash text;

ALTER TABLE trial_results
    ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT clock_timestamp();

ALTER TABLE logical_trials
    ADD COLUMN IF NOT EXISTS case_id text,
    ADD COLUMN IF NOT EXISTS repetition_index integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS model_hash text,
    ADD COLUMN IF NOT EXISTS environment_hash text,
    ADD COLUMN IF NOT EXISTS grader_hash text;

CREATE INDEX IF NOT EXISTS logical_trials_case_id_idx
    ON logical_trials (experiment_id, case_id);

-- +goose Down
DROP INDEX IF EXISTS logical_trials_case_id_idx;

ALTER TABLE logical_trials
    DROP COLUMN IF EXISTS grader_hash,
    DROP COLUMN IF EXISTS environment_hash,
    DROP COLUMN IF EXISTS model_hash,
    DROP COLUMN IF EXISTS repetition_index,
    DROP COLUMN IF EXISTS case_id;

ALTER TABLE experiments
    DROP COLUMN IF EXISTS grader_hash;

ALTER TABLE trial_results
    DROP COLUMN IF EXISTS updated_at;
