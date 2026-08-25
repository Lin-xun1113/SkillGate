-- +goose Up
ALTER TABLE trial_attempts
    ADD COLUMN IF NOT EXISTS request_hash text,
    ADD COLUMN IF NOT EXISTS usage jsonb NOT NULL DEFAULT '{}';

ALTER TABLE trial_results
    ADD COLUMN IF NOT EXISTS request_hash text,
    ADD COLUMN IF NOT EXISTS usage jsonb NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS artifacts jsonb NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS grades jsonb NOT NULL DEFAULT '{}';

CREATE TABLE IF NOT EXISTS trial_events (
    event_id text PRIMARY KEY,
    trial_id text NOT NULL REFERENCES trial_attempts(trial_id) ON DELETE RESTRICT,
    logical_trial_id text NOT NULL REFERENCES logical_trials(logical_trial_id) ON DELETE RESTRICT,
    experiment_id text NOT NULL REFERENCES experiments(experiment_id) ON DELETE RESTRICT,
    attempt_no integer NOT NULL CHECK (attempt_no >= 1),
    worker_id text NOT NULL,
    sequence bigint NOT NULL CHECK (sequence > 0),
    event_type text NOT NULL,
    occurred_at timestamptz NOT NULL,
    payload jsonb NOT NULL,
    payload_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (trial_id, sequence)
);

CREATE INDEX IF NOT EXISTS trial_events_trial_sequence_idx
    ON trial_events (trial_id, sequence);

-- +goose Down
DROP TABLE IF EXISTS trial_events;
ALTER TABLE trial_results
    DROP COLUMN IF EXISTS grades,
    DROP COLUMN IF EXISTS artifacts,
    DROP COLUMN IF EXISTS usage,
    DROP COLUMN IF EXISTS request_hash;
ALTER TABLE trial_attempts
    DROP COLUMN IF EXISTS usage,
    DROP COLUMN IF EXISTS request_hash;
