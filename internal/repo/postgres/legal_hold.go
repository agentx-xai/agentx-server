package postgres

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type LegalHoldRepository struct{ *Store }

const legalHoldColumns = `id::text,target_type,target_id,reason,COALESCE(created_by,''),created_at,COALESCE(released_by,''),released_at,COALESCE(release_reason,'')`

type legalHoldScanner interface {
	Scan(...any) error
}

func scanLegalHold(scanner legalHoldScanner) (entity.LegalHold, error) {
	var hold entity.LegalHold
	err := scanner.Scan(&hold.ID, &hold.TargetType, &hold.TargetID, &hold.Reason, &hold.CreatedBy, &hold.CreatedAt, &hold.ReleasedBy, &hold.ReleasedAt, &hold.ReleaseReason)
	return hold, err
}

func (r LegalHoldRepository) CreateLegalHold(ctx context.Context, hold entity.LegalHold, audit entity.AuditEvent, outbox entity.OutboxEvent) error {
	return r.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := lockLegalHoldRetention(txCtx, r.Store); err != nil {
			return err
		}
		if err := lockLegalHoldTarget(txCtx, r.Store, hold.TargetType, hold.TargetID); err != nil {
			return err
		}
		exists, err := r.targetExists(txCtx, hold.TargetType, hold.TargetID)
		if err != nil {
			return err
		}
		if !exists {
			return repo.NotFound(hold.TargetType)
		}
		_, err = r.exec(txCtx, `INSERT INTO legal_holds(id,target_type,target_id,reason,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6)`, hold.ID, hold.TargetType, hold.TargetID, hold.Reason, hold.CreatedBy, hold.CreatedAt)
		if err != nil {
			return conflictOnUnique(err, "an active legal hold already exists for this target")
		}
		if err = appendLegalHoldAudit(txCtx, r.Store, audit); err != nil {
			return err
		}
		return (OutboxRepo{Store: r.Store}).Enqueue(txCtx, outbox)
	})
}

func (r LegalHoldRepository) LegalHold(ctx context.Context, id string) (entity.LegalHold, error) {
	hold, err := scanLegalHold(r.queryRow(ctx, `SELECT `+legalHoldColumns+` FROM legal_holds WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.LegalHold{}, repo.NotFound("legal hold")
	}
	return hold, err
}

func (r LegalHoldRepository) ListLegalHoldsPage(ctx context.Context, filter repo.LegalHoldFilter, request repo.PageRequest) (repo.Page[entity.LegalHold], error) {
	var page repo.Page[entity.LegalHold]
	if err := r.queryRow(ctx, `SELECT count(*) FROM legal_holds WHERE ($1='' OR ($1='active' AND released_at IS NULL) OR ($1='released' AND released_at IS NOT NULL)) AND ($2='' OR target_type=$2) AND ($3='' OR target_id=$3)`, filter.Status, filter.TargetType, filter.TargetID).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := r.query(ctx, `SELECT `+legalHoldColumns+` FROM legal_holds WHERE ($1='' OR ($1='active' AND released_at IS NULL) OR ($1='released' AND released_at IS NOT NULL)) AND ($2='' OR target_type=$2) AND ($3='' OR target_id=$3) ORDER BY created_at DESC,id LIMIT $4 OFFSET $5`, filter.Status, filter.TargetType, filter.TargetID, request.Limit, request.Offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	page.Items = make([]entity.LegalHold, 0, request.Limit)
	for rows.Next() {
		hold, scanErr := scanLegalHold(rows)
		if scanErr != nil {
			return repo.Page[entity.LegalHold]{}, scanErr
		}
		page.Items = append(page.Items, hold)
	}
	return page, rows.Err()
}

func (r LegalHoldRepository) ReleaseLegalHold(ctx context.Context, id, releasedBy, reason string, at time.Time, audit entity.AuditEvent, outbox entity.OutboxEvent) (entity.LegalHold, error) {
	var released entity.LegalHold
	err := r.WithinTransaction(ctx, func(txCtx context.Context) error {
		hold, err := scanLegalHold(r.queryRow(txCtx, `SELECT `+legalHoldColumns+` FROM legal_holds WHERE id=$1 FOR UPDATE`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.NotFound("legal hold")
		}
		if err != nil {
			return err
		}
		if hold.ReleasedAt != nil {
			return repo.Conflict("legal hold is already released")
		}
		if err = lockLegalHoldTarget(txCtx, r.Store, hold.TargetType, hold.TargetID); err != nil {
			return err
		}
		result, err := r.exec(txCtx, `UPDATE legal_holds SET released_by=$2,released_at=$3,release_reason=$4 WHERE id=$1 AND released_at IS NULL`, id, releasedBy, at, reason)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return repo.Conflict("legal hold is already released")
		}
		hold.ReleasedBy, hold.ReleasedAt, hold.ReleaseReason = releasedBy, &at, reason
		if err = appendLegalHoldAudit(txCtx, r.Store, audit); err != nil {
			return err
		}
		if err = (OutboxRepo{Store: r.Store}).Enqueue(txCtx, outbox); err != nil {
			return err
		}
		released = hold
		return nil
	})
	return released, err
}

func (r LegalHoldRepository) targetExists(ctx context.Context, targetType, targetID string) (bool, error) {
	var exists bool
	var err error
	switch targetType {
	case entity.LegalHoldTargetAccount:
		err = r.queryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, targetID).Scan(&exists)
	case entity.LegalHoldTargetWorkspace:
		err = r.queryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspaces WHERE id=$1::uuid)`, targetID).Scan(&exists)
	default:
		return false, repo.NotFound("target")
	}
	return exists, err
}

func appendLegalHoldAudit(ctx context.Context, store *Store, audit entity.AuditEvent) error {
	metadata, err := json.Marshal(audit.Metadata)
	if err != nil {
		return err
	}
	_, err = store.exec(ctx, `INSERT INTO audit_events(workspace_id,actor_id,action,resource_type,resource_id,metadata_json,request_id,created_at) VALUES(NULL,$1,$2,$3,$4,$5,$6,$7)`, audit.ActorID, audit.Action, audit.ResourceType, audit.ResourceID, metadata, audit.RequestID, audit.At)
	return err
}

func lockLegalHoldTarget(ctx context.Context, store *Store, targetType, targetID string) error {
	_, err := store.exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "agentx-legal-hold\n"+targetType+"\n"+targetID)
	return err
}

func ensureDeletionNotHeld(ctx context.Context, store *Store, targetType, targetID string) error {
	if err := lockLegalHoldTarget(ctx, store, targetType, targetID); err != nil {
		return err
	}
	var held bool
	if err := store.queryRow(ctx, `SELECT EXISTS(SELECT 1 FROM legal_holds WHERE target_type=$1 AND target_id=$2 AND released_at IS NULL)`, targetType, targetID).Scan(&held); err != nil {
		return err
	}
	if held {
		return repo.Conflict(targetType + " is under an active legal hold")
	}
	return nil
}

type AuditRetentionRepository struct{ *Store }

func (r AuditRetentionRepository) PruneAuditEvents(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		limit = 1000
	}
	var deleted int64
	err := r.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := lockLegalHoldRetention(txCtx, r.Store); err != nil {
			return err
		}
		result, err := r.exec(txCtx, `
WITH expired AS (
    SELECT event.id
    FROM audit_events event
    WHERE event.created_at < $1
      AND event.action NOT IN ('account.delete','workspace.delete','legal_hold.create','legal_hold.release')
      AND NOT EXISTS (
          SELECT 1 FROM legal_holds hold
          WHERE hold.released_at IS NULL
            AND ((hold.target_type='workspace' AND hold.target_id=event.workspace_id::text)
              OR (hold.target_type='account' AND hold.target_id=event.actor_id))
      )
    ORDER BY event.created_at,event.id
    FOR UPDATE SKIP LOCKED
    LIMIT $2
)
DELETE FROM audit_events event USING expired WHERE event.id=expired.id`, before, limit)
		if err != nil {
			return err
		}
		deleted = result.RowsAffected()
		return nil
	})
	return deleted, err
}

func lockLegalHoldRetention(ctx context.Context, store *Store) error {
	_, err := store.exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "agentx-legal-hold-retention")
	return err
}
