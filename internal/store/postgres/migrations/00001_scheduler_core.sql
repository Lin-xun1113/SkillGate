-- +goose Up
CREATE TABLE experiments (
    experiment_id text PRIMARY KEY,
    manifest_hash text NOT NULL,
    plan_hash text NOT NULL,
    status text NOT NULL CHECK (status IN ('COMPILED', 'QUEUED', 'RUNNING', 'CANCEL_REQUESTED', 'GRADING', 'CANCELLED', 'FAILED')),
    total_logical_trials integer NOT NULL CHECK (total_logical_trials >= 0),
    terminal_count integer NOT NULL DEFAULT 0 CHECK (terminal_count >= 0),
    succeeded_count integer NOT NULL DEFAULT 0 CHECK (succeeded_count >= 0),
    failed_count integer NOT NULL DEFAULT 0 CHECK (failed_count >= 0),
    timed_out_count integer NOT NULL DEFAULT 0 CHECK (timed_out_count >= 0),
    cancelled_count integer NOT NULL DEFAULT 0 CHECK (cancelled_count >= 0),
    budget_deadline_at timestamptz NOT NULL,
    cancel_requested_at timestamptz,
    cancel_reason text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT experiments_manifest_hash_uq UNIQUE (manifest_hash),
    CONSTRAINT experiments_counter_sum_ck CHECK (
        terminal_count = succeeded_count + failed_count + timed_out_count + cancelled_count
        AND terminal_count <= total_logical_trials
    )
);

CREATE TABLE logical_trials (
    logical_trial_id text PRIMARY KEY,
    experiment_id text NOT NULL REFERENCES experiments(experiment_id) ON DELETE RESTRICT,
    pair_id text NOT NULL,
    arm text NOT NULL,
    status text NOT NULL CHECK (status IN ('PENDING', 'LEASED', 'RUNNING', 'RETRY_WAIT', 'SUCCEEDED', 'FAILED', 'TIMED_OUT', 'CANCELLED')),
    priority integer NOT NULL DEFAULT 0,
    current_attempt integer NOT NULL DEFAULT 1 CHECK (current_attempt >= 1),
    max_attempts integer NOT NULL CHECK (max_attempts BETWEEN 1 AND 30),
    timeout_ms bigint NOT NULL CHECK (timeout_ms > 0),
    backoff_base_ms bigint NOT NULL CHECK (backoff_base_ms > 0),
    backoff_cap_ms bigint NOT NULL CHECK (backoff_cap_ms >= backoff_base_ms),
    retryable_categories text[] NOT NULL DEFAULT '{}',
    lease_generation bigint NOT NULL DEFAULT 0 CHECK (lease_generation >= 0),
    final_result_id text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT logical_trials_pair_arm_uq UNIQUE (experiment_id, pair_id, arm)
);

CREATE TABLE trial_attempts (
    trial_id text PRIMARY KEY,
    logical_trial_id text NOT NULL REFERENCES logical_trials(logical_trial_id) ON DELETE RESTRICT,
    attempt_no integer NOT NULL CHECK (attempt_no >= 1),
    status text NOT NULL CHECK (status IN ('PENDING', 'LEASED', 'RUNNING', 'SUCCEEDED', 'FAILED', 'TIMED_OUT', 'CANCELLED')),
    priority integer NOT NULL DEFAULT 0,
    not_before timestamptz NOT NULL DEFAULT clock_timestamp(),
    lease_owner text,
    lease_token_hash bytea,
    lease_generation bigint,
    lease_expires_at timestamptz,
    heartbeat_at timestamptz,
    started_at timestamptz,
    deadline_at timestamptz,
    cancel_requested_at timestamptz,
    event_sequence bigint NOT NULL DEFAULT 0 CHECK (event_sequence >= 0),
    phase text,
    failure_category text,
    outcome_manifest_hash text,
    outcome_manifest jsonb,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT trial_attempts_logical_attempt_uq UNIQUE (logical_trial_id, attempt_no),
    CONSTRAINT trial_attempts_lease_group_ck CHECK (
        (lease_owner IS NULL AND lease_token_hash IS NULL AND lease_generation IS NULL AND lease_expires_at IS NULL AND heartbeat_at IS NULL)
        OR
        (lease_owner IS NOT NULL AND lease_token_hash IS NOT NULL AND lease_generation IS NOT NULL AND lease_expires_at IS NOT NULL AND heartbeat_at IS NOT NULL)
    )
);

CREATE TABLE trial_results (
    result_id text PRIMARY KEY,
    logical_trial_id text NOT NULL UNIQUE REFERENCES logical_trials(logical_trial_id) ON DELETE RESTRICT,
    trial_id text NOT NULL UNIQUE REFERENCES trial_attempts(trial_id) ON DELETE RESTRICT,
    idempotency_key text NOT NULL UNIQUE,
    manifest_hash text NOT NULL,
    manifest jsonb NOT NULL,
    outcome text NOT NULL CHECK (outcome IN ('SUCCEEDED', 'FAILED', 'TIMED_OUT', 'CANCELLED')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

ALTER TABLE logical_trials
    ADD CONSTRAINT logical_trials_final_result_fk
    FOREIGN KEY (final_result_id) REFERENCES trial_results(result_id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE trial_transition_events (
    event_id bigserial PRIMARY KEY,
    experiment_id text NOT NULL REFERENCES experiments(experiment_id) ON DELETE RESTRICT,
    logical_trial_id text REFERENCES logical_trials(logical_trial_id) ON DELETE RESTRICT,
    trial_id text REFERENCES trial_attempts(trial_id) ON DELETE RESTRICT,
    actor text NOT NULL,
    from_status text,
    to_status text NOT NULL,
    reason_code text NOT NULL,
    detail jsonb NOT NULL DEFAULT '{}',
    occurred_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX trial_attempts_claim_idx
    ON trial_attempts (priority DESC, not_before, created_at, trial_id)
    WHERE status = 'PENDING';

CREATE INDEX trial_attempts_expiry_idx
    ON trial_attempts (lease_expires_at, trial_id)
    WHERE status IN ('LEASED', 'RUNNING');

CREATE INDEX logical_trials_experiment_status_idx
    ON logical_trials (experiment_id, status, logical_trial_id);

CREATE INDEX trial_transition_events_trial_idx
    ON trial_transition_events (logical_trial_id, occurred_at, event_id);

-- +goose Down
DROP TABLE trial_transition_events;
ALTER TABLE logical_trials DROP CONSTRAINT logical_trials_final_result_fk;
DROP TABLE trial_results;
DROP TABLE trial_attempts;
DROP TABLE logical_trials;
DROP TABLE experiments;
