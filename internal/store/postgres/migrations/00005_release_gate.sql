-- +goose Up
ALTER TABLE experiments
    ADD COLUMN IF NOT EXISTS policy_hash text;

ALTER TABLE logical_trials
    ADD COLUMN IF NOT EXISTS evaluation_mode text,
    ADD COLUMN IF NOT EXISTS population text,
    ADD COLUMN IF NOT EXISTS polarity text;

CREATE TABLE IF NOT EXISTS metrics_snapshots (
    snapshot_id text PRIMARY KEY,
    experiment_id text NOT NULL UNIQUE REFERENCES experiments(experiment_id) ON DELETE RESTRICT,
    snapshot_hash text NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE IF NOT EXISTS release_decisions (
    decision_id text PRIMARY KEY,
    experiment_id text NOT NULL UNIQUE REFERENCES experiments(experiment_id) ON DELETE RESTRICT,
    result text NOT NULL CHECK (result IN ('PROMOTE', 'HOLD', 'REJECT')),
    policy_id text NOT NULL,
    policy_version text NOT NULL,
    policy_hash text NOT NULL,
    snapshot_hash text NOT NULL,
    explanation text NOT NULL,
    trace jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- +goose Down
DROP TABLE IF EXISTS release_decisions;
DROP TABLE IF EXISTS metrics_snapshots;

ALTER TABLE experiments
    DROP COLUMN IF EXISTS policy_hash;
