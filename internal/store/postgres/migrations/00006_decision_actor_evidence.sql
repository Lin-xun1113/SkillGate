-- +goose Up
ALTER TABLE release_decisions
    ADD COLUMN IF NOT EXISTS actor text NOT NULL DEFAULT 'grading-service',
    ADD COLUMN IF NOT EXISTS evidence_links jsonb NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down
ALTER TABLE release_decisions
    DROP COLUMN IF EXISTS evidence_links,
    DROP COLUMN IF EXISTS actor;
