-- +goose Up
CREATE TABLE legal_holds (
    id uuid PRIMARY KEY,
    target_type text NOT NULL CHECK (target_type IN ('account','workspace')),
    target_id text NOT NULL CHECK (target_id <> ''),
    reason text NOT NULL CHECK (reason <> ''),
    created_by text,
    created_at timestamptz NOT NULL,
    released_by text,
    released_at timestamptz,
    release_reason text,
    CHECK ((released_at IS NULL AND released_by IS NULL AND release_reason IS NULL) OR
           (released_at IS NOT NULL AND release_reason IS NOT NULL AND release_reason <> ''))
);

CREATE UNIQUE INDEX legal_holds_active_target_idx
    ON legal_holds(target_type, target_id) WHERE released_at IS NULL;
CREATE INDEX legal_holds_created_idx ON legal_holds(created_at DESC, id);

-- +goose Down
DROP TABLE IF EXISTS legal_holds;
