-- +goose Up
ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_workspace_id_fkey;
ALTER TABLE audit_events
    ADD CONSTRAINT audit_events_workspace_id_fkey
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_workspace_id_fkey;
ALTER TABLE audit_events
    ADD CONSTRAINT audit_events_workspace_id_fkey
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE;
