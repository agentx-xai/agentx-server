-- +goose Up
ALTER TABLE workspace_invitations DROP CONSTRAINT IF EXISTS workspace_invitations_created_by_fkey;
ALTER TABLE workspace_invitations DROP CONSTRAINT IF EXISTS workspace_invitations_revoked_by_fkey;
ALTER TABLE workspace_invitations DROP CONSTRAINT IF EXISTS workspace_invitations_accepted_by_fkey;
ALTER TABLE workspace_invitations ALTER COLUMN created_by DROP NOT NULL;
ALTER TABLE workspace_invitations
    ADD CONSTRAINT workspace_invitations_created_by_fkey FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE workspace_invitations
    ADD CONSTRAINT workspace_invitations_revoked_by_fkey FOREIGN KEY (revoked_by) REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE workspace_invitations
    ADD CONSTRAINT workspace_invitations_accepted_by_fkey FOREIGN KEY (accepted_by) REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS audit_events_actor_created_idx ON audit_events(actor_id, created_at DESC) WHERE actor_id IS NOT NULL;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM workspace_invitations WHERE created_by IS NULL) THEN
        RAISE EXCEPTION 'cannot roll back: deleted invitation creators have been anonymized';
    END IF;
END $$;
-- +goose StatementEnd

DROP INDEX IF EXISTS audit_events_actor_created_idx;
ALTER TABLE workspace_invitations DROP CONSTRAINT IF EXISTS workspace_invitations_created_by_fkey;
ALTER TABLE workspace_invitations DROP CONSTRAINT IF EXISTS workspace_invitations_revoked_by_fkey;
ALTER TABLE workspace_invitations DROP CONSTRAINT IF EXISTS workspace_invitations_accepted_by_fkey;
ALTER TABLE workspace_invitations ALTER COLUMN created_by SET NOT NULL;
ALTER TABLE workspace_invitations
    ADD CONSTRAINT workspace_invitations_created_by_fkey FOREIGN KEY (created_by) REFERENCES users(id);
ALTER TABLE workspace_invitations
    ADD CONSTRAINT workspace_invitations_revoked_by_fkey FOREIGN KEY (revoked_by) REFERENCES users(id);
ALTER TABLE workspace_invitations
    ADD CONSTRAINT workspace_invitations_accepted_by_fkey FOREIGN KEY (accepted_by) REFERENCES users(id);
