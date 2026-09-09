-- +goose Up
ALTER TABLE idempotency_keys
    ADD COLUMN IF NOT EXISTS resource_type text NOT NULL DEFAULT 'release';

-- +goose Down
ALTER TABLE idempotency_keys DROP COLUMN IF EXISTS resource_type;
