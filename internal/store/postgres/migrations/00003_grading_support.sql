-- +goose Up
-- Add GRADING status to experiments
ALTER TABLE experiments
    DROP CONSTRAINT IF EXISTS experiments_status_check;

ALTER TABLE experiments
    ADD CONSTRAINT experiments_status_check
    CHECK (status IN ('COMPILED', 'QUEUED', 'RUNNING', 'GRADING', 'COMPLETED', 'CANCEL_REQUESTED', 'CANCELLED', 'FAILED'));

-- Create experiment_reports table
CREATE TABLE IF NOT EXISTS experiment_reports (
    report_id text PRIMARY KEY,
    experiment_id text NOT NULL UNIQUE REFERENCES experiments(experiment_id) ON DELETE RESTRICT,
    report_type text NOT NULL CHECK (report_type IN ('json', 'markdown', 'html')),
    file_path text NOT NULL,
    file_hash text NOT NULL,
    valid_pairs int NOT NULL CHECK (valid_pairs >= 0),
    invalid_pairs int NOT NULL CHECK (invalid_pairs >= 0),
    mean_lift float8,
    ci_lower float8,
    ci_upper float8,
    statistically_significant boolean,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX IF NOT EXISTS experiment_reports_experiment_idx
    ON experiment_reports (experiment_id);

-- +goose Down
DROP TABLE IF EXISTS experiment_reports;

ALTER TABLE experiments
    DROP CONSTRAINT IF EXISTS experiments_status_check;

ALTER TABLE experiments
    ADD CONSTRAINT experiments_status_check
    CHECK (status IN ('COMPILED', 'QUEUED', 'RUNNING', 'CANCEL_REQUESTED', 'CANCELLED', 'FAILED'));
