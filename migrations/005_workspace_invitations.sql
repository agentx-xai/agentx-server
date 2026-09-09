-- +goose Up
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified boolean NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS workspace_invitations (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    email text NOT NULL CHECK (email = lower(email) AND length(email) <= 254),
    role text NOT NULL CHECK (role IN ('admin','developer','viewer')),
    created_by text NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at),
    revoked_at timestamptz,
    revoked_by text REFERENCES users(id),
    accepted_at timestamptz,
    accepted_by text REFERENCES users(id),
    CHECK (revoked_at IS NULL OR accepted_at IS NULL)
);

CREATE UNIQUE INDEX IF NOT EXISTS workspace_invitations_pending_email_idx
    ON workspace_invitations(workspace_id, email)
    WHERE revoked_at IS NULL AND accepted_at IS NULL;
CREATE INDEX IF NOT EXISTS workspace_invitations_email_status_idx
    ON workspace_invitations(email, expires_at DESC)
    WHERE revoked_at IS NULL AND accepted_at IS NULL;
CREATE INDEX IF NOT EXISTS workspace_invitations_workspace_created_idx
    ON workspace_invitations(workspace_id, created_at DESC, id);

-- +goose Down
DROP TABLE IF EXISTS workspace_invitations;
ALTER TABLE users DROP COLUMN IF EXISTS email_verified;
