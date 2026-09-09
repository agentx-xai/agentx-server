package postgres

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type AccountRepository struct{ *Store }

func (r AccountRepository) ExportAccount(ctx context.Context, userID, currentEmail string) (entity.AccountExport, error) {
	export := entity.AccountExport{
		Memberships: []entity.AccountMembership{}, Invitations: []entity.WorkspaceInvitation{}, AuditEvents: []entity.AccountAuditEvent{},
	}
	storedEmail := ""
	err := r.queryRow(ctx, `SELECT id,issuer,subject,COALESCE(email,''),email_verified,created_at FROM users WHERE id=$1`, userID).
		Scan(&export.User.ID, &export.User.Issuer, &export.User.Subject, &export.User.Email, &export.User.EmailVerified, &export.User.CreatedAt)
	if err == nil {
		storedEmail = export.User.Email
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return entity.AccountExport{}, err
	}

	rows, err := r.query(ctx, `SELECT w.id,w.slug,w.name,w.created_at,m.role,m.created_at FROM memberships m JOIN workspaces w ON w.id=m.workspace_id WHERE m.user_id=$1 ORDER BY w.name,w.id`, userID)
	if err != nil {
		return entity.AccountExport{}, err
	}
	for rows.Next() {
		var item entity.AccountMembership
		if err = rows.Scan(&item.Workspace.ID, &item.Workspace.Slug, &item.Workspace.Name, &item.Workspace.CreatedAt, &item.Role, &item.CreatedAt); err != nil {
			rows.Close()
			return entity.AccountExport{}, err
		}
		export.Memberships = append(export.Memberships, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return entity.AccountExport{}, err
	}
	rows.Close()

	if currentEmail != "" || storedEmail != "" {
		now := time.Now().UTC()
		rows, err = r.query(ctx, `SELECT `+invitationColumns+` FROM workspace_invitations i JOIN workspaces w ON w.id=i.workspace_id WHERE ($1::text<>'' AND i.email=$1::text) OR ($2::text<>'' AND i.email=$2::text) ORDER BY i.created_at DESC,i.id`, currentEmail, storedEmail)
		if err != nil {
			return entity.AccountExport{}, err
		}
		for rows.Next() {
			invitation, scanErr := scanInvitation(rows, now)
			if scanErr != nil {
				rows.Close()
				return entity.AccountExport{}, scanErr
			}
			export.Invitations = append(export.Invitations, invitation)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return entity.AccountExport{}, err
		}
		rows.Close()
	}

	rows, err = r.query(ctx, `SELECT COALESCE(workspace_id::text,''),action,COALESCE(resource_type,''),COALESCE(resource_id,''),COALESCE(request_id,''),created_at FROM audit_events WHERE actor_id=$1 ORDER BY created_at DESC,id`, userID)
	if err != nil {
		return entity.AccountExport{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var event entity.AccountAuditEvent
		if err = rows.Scan(&event.WorkspaceID, &event.Action, &event.ResourceType, &event.ResourceID, &event.RequestID, &event.At); err != nil {
			return entity.AccountExport{}, err
		}
		export.AuditEvents = append(export.AuditEvents, event)
	}
	return export, rows.Err()
}

func (r AccountRepository) DeleteAccount(ctx context.Context, deletion entity.AccountDeletion) error {
	return r.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := lockAccount(txCtx, r.Store, deletion.UserID); err != nil {
			return err
		}
		if err := ensureDeletionNotHeld(txCtx, r.Store, entity.LegalHoldTargetAccount, deletion.UserID); err != nil {
			return err
		}
		var ownsWorkspace bool
		if err := r.queryRow(txCtx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE user_id=$1 AND role='owner')`, deletion.UserID).Scan(&ownsWorkspace); err != nil {
			return err
		}
		if ownsWorkspace {
			return repo.Conflict("account owns a workspace")
		}
		storedEmail := ""
		err := r.queryRow(txCtx, `SELECT COALESCE(email,'') FROM users WHERE id=$1`, deletion.UserID).Scan(&storedEmail)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err = r.exec(txCtx, `UPDATE audit_events SET actor_id=NULL WHERE actor_id=$1`, deletion.UserID); err != nil {
			return err
		}
		if _, err = r.exec(txCtx, `DELETE FROM workspace_invitations WHERE ($1::text<>'' AND email=$1::text) OR ($2::text<>'' AND email=$2::text)`, deletion.Email, storedEmail); err != nil {
			return err
		}
		if _, err = r.exec(txCtx, `UPDATE workspace_invitations SET created_by=NULL WHERE created_by=$1`, deletion.UserID); err != nil {
			return err
		}
		if _, err = r.exec(txCtx, `UPDATE workspace_invitations SET revoked_by=NULL WHERE revoked_by=$1`, deletion.UserID); err != nil {
			return err
		}
		if _, err = r.exec(txCtx, `UPDATE workspace_invitations SET accepted_by=NULL WHERE accepted_by=$1`, deletion.UserID); err != nil {
			return err
		}
		if _, err = r.exec(txCtx, `UPDATE legal_holds SET created_by=NULL WHERE created_by=$1`, deletion.UserID); err != nil {
			return err
		}
		if _, err = r.exec(txCtx, `UPDATE legal_holds SET released_by=NULL WHERE released_by=$1`, deletion.UserID); err != nil {
			return err
		}
		anonymousTarget := "deleted-account:" + deletion.DeletionID
		if _, err = r.exec(txCtx, `UPDATE legal_holds SET target_id=$2 WHERE target_type='account' AND target_id=$1 AND released_at IS NOT NULL`, deletion.UserID, anonymousTarget); err != nil {
			return err
		}
		if _, err = r.exec(txCtx, `UPDATE audit_events SET metadata_json=jsonb_set(metadata_json,'{target_id}',to_jsonb($2::text)) WHERE metadata_json->>'target_type'='account' AND metadata_json->>'target_id'=$1`, deletion.UserID, anonymousTarget); err != nil {
			return err
		}
		if _, err = r.exec(txCtx, `UPDATE outbox SET payload_json=jsonb_set(payload_json,'{target_id}',to_jsonb($2::text)) WHERE payload_json->>'target_type'='account' AND payload_json->>'target_id'=$1`, deletion.UserID, anonymousTarget); err != nil {
			return err
		}
		if _, err = r.exec(txCtx, `DELETE FROM memberships WHERE user_id=$1`, deletion.UserID); err != nil {
			return err
		}
		if _, err = r.exec(txCtx, `UPDATE outbox SET payload_json=payload_json-'user_id' WHERE payload_json->>'user_id'=$1`, deletion.UserID); err != nil {
			return err
		}
		if _, err = r.exec(txCtx, `DELETE FROM users WHERE id=$1`, deletion.UserID); err != nil {
			return err
		}
		if _, err = r.exec(txCtx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,metadata_json,request_id,created_at) VALUES(NULL,'account.delete','account_deletion',$1,'{}',$2,$3)`, deletion.DeletionID, deletion.RequestID, deletion.At); err != nil {
			return err
		}
		_, err = r.exec(txCtx, `INSERT INTO outbox(id,topic,payload_json,available_at) VALUES($1::uuid,'account.deleted',jsonb_build_object('deletion_id',($1::uuid)::text),$2)`, deletion.DeletionID, deletion.At)
		return err
	})
}

func lockAccount(ctx context.Context, store *Store, userID string) error {
	_, err := store.exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "agentx-account\n"+userID)
	return err
}
