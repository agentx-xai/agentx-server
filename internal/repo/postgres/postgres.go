package postgres

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"agentx/server/internal/telemetry"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
)

type Store struct{ Pool *pgxpool.Pool }

type transactionContextKey struct{}

func Open(ctx context.Context, url string) (*Store, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	config.ConnConfig.Tracer = telemetry.NewPGXTracer()
	p, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	if err = p.Ping(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return &Store{Pool: p}, nil
}
func (s *Store) Close() { s.Pool.Close() }

func (s *Store) WithinTransaction(ctx context.Context, operation func(context.Context) error) error {
	if _, ok := ctx.Value(transactionContextKey{}).(pgx.Tx); ok {
		return operation(ctx)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = operation(context.WithValue(ctx, transactionContextKey{}, tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	if tx, ok := ctx.Value(transactionContextKey{}).(pgx.Tx); ok {
		return tx.Exec(ctx, sql, arguments...)
	}
	return s.Pool.Exec(ctx, sql, arguments...)
}

func (s *Store) queryRow(ctx context.Context, sql string, arguments ...any) pgx.Row {
	if tx, ok := ctx.Value(transactionContextKey{}).(pgx.Tx); ok {
		return tx.QueryRow(ctx, sql, arguments...)
	}
	return s.Pool.QueryRow(ctx, sql, arguments...)
}

func (s *Store) query(ctx context.Context, sql string, arguments ...any) (pgx.Rows, error) {
	if tx, ok := ctx.Value(transactionContextKey{}).(pgx.Tx); ok {
		return tx.Query(ctx, sql, arguments...)
	}
	return s.Pool.Query(ctx, sql, arguments...)
}

type WorkspaceRepository struct{ *Store }

func (r WorkspaceRepository) List(ctx context.Context, userID string) ([]entity.Workspace, error) {
	rows, err := r.Pool.Query(ctx, `SELECT w.id,w.slug,w.name,w.created_at FROM workspaces w JOIN memberships m ON m.workspace_id=w.id WHERE m.user_id=$1 ORDER BY w.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entity.Workspace
	for rows.Next() {
		var w entity.Workspace
		if err := rows.Scan(&w.ID, &w.Slug, &w.Name, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
func (r WorkspaceRepository) ListPage(ctx context.Context, userID string, request repo.PageRequest) (repo.Page[entity.Workspace], error) {
	var page repo.Page[entity.Workspace]
	if err := r.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM memberships WHERE user_id=$1`, userID).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := r.Pool.Query(ctx, `SELECT w.id,w.slug,w.name,w.created_at FROM workspaces w JOIN memberships m ON m.workspace_id=w.id WHERE m.user_id=$1 ORDER BY w.name,w.id LIMIT $2 OFFSET $3`, userID, request.Limit, request.Offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	page.Items = make([]entity.Workspace, 0, request.Limit)
	for rows.Next() {
		var workspace entity.Workspace
		if err := rows.Scan(&workspace.ID, &workspace.Slug, &workspace.Name, &workspace.CreatedAt); err != nil {
			return repo.Page[entity.Workspace]{}, err
		}
		page.Items = append(page.Items, workspace)
	}
	return page, rows.Err()
}
func (r WorkspaceRepository) Create(ctx context.Context, w entity.Workspace, m entity.Membership) error {
	return r.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := lockAccount(txCtx, r.Store, m.UserID); err != nil {
			return err
		}
		if err := ensureUser(txCtx, r.Store, m.UserID); err != nil {
			return err
		}
		if _, err := r.exec(txCtx, `INSERT INTO workspaces(id,slug,name,created_at) VALUES($1,$2,$3,$4)`, w.ID, w.Slug, w.Name, w.CreatedAt); err != nil {
			return conflictOnUnique(err, "workspace slug already exists")
		}
		_, err := r.exec(txCtx, `INSERT INTO memberships(workspace_id,user_id,role,created_at) VALUES($1,$2,$3,$4)`, m.WorkspaceID, m.UserID, m.Role, m.CreatedAt)
		return err
	})
}
func (r WorkspaceRepository) Membership(ctx context.Context, wid, uid string) (entity.Membership, error) {
	var m entity.Membership
	err := r.Pool.QueryRow(ctx, `SELECT workspace_id,user_id,role,created_at FROM memberships WHERE workspace_id=$1 AND user_id=$2`, wid, uid).Scan(&m.WorkspaceID, &m.UserID, &m.Role, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, repo.NotFound("membership")
	}
	return m, err
}
func (r WorkspaceRepository) Members(ctx context.Context, wid string) ([]entity.Membership, error) {
	rows, err := r.Pool.Query(ctx, `SELECT workspace_id,user_id,role,created_at FROM memberships WHERE workspace_id=$1 ORDER BY created_at`, wid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entity.Membership
	for rows.Next() {
		var m entity.Membership
		if err := rows.Scan(&m.WorkspaceID, &m.UserID, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (r WorkspaceRepository) MembersPage(ctx context.Context, wid string, request repo.PageRequest) (repo.Page[entity.Membership], error) {
	var page repo.Page[entity.Membership]
	if err := r.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM memberships WHERE workspace_id=$1`, wid).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := r.Pool.Query(ctx, `SELECT workspace_id,user_id,role,created_at FROM memberships WHERE workspace_id=$1 ORDER BY created_at,user_id LIMIT $2 OFFSET $3`, wid, request.Limit, request.Offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	page.Items = make([]entity.Membership, 0, request.Limit)
	for rows.Next() {
		var membership entity.Membership
		if err := rows.Scan(&membership.WorkspaceID, &membership.UserID, &membership.Role, &membership.CreatedAt); err != nil {
			return repo.Page[entity.Membership]{}, err
		}
		page.Items = append(page.Items, membership)
	}
	return page, rows.Err()
}
func (r WorkspaceRepository) AddMember(ctx context.Context, m entity.Membership) error {
	return r.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := lockAccount(txCtx, r.Store, m.UserID); err != nil {
			return err
		}
		if err := ensureUser(txCtx, r.Store, m.UserID); err != nil {
			return err
		}
		_, err := r.exec(txCtx, `INSERT INTO memberships(workspace_id,user_id,role,created_at) VALUES($1,$2,$3,$4)`, m.WorkspaceID, m.UserID, m.Role, m.CreatedAt)
		return conflictOnUnique(err, "membership already exists")
	})
}
func (r WorkspaceRepository) UpdateMemberRole(ctx context.Context, workspaceID, userID string, role entity.Role) error {
	result, err := r.exec(ctx, `UPDATE memberships SET role=$3 WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID, role)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return repo.NotFound("membership")
	}
	return nil
}
func (r WorkspaceRepository) RemoveMember(ctx context.Context, workspaceID, userID string) error {
	result, err := r.exec(ctx, `DELETE FROM memberships WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return repo.NotFound("membership")
	}
	return nil
}

const invitationColumns = `i.id,i.workspace_id,i.email,i.role,COALESCE(i.created_by,''),i.created_at,i.expires_at,i.revoked_at,COALESCE(i.revoked_by,''),i.accepted_at,COALESCE(i.accepted_by,''),w.name,w.slug`

type rowScanner interface {
	Scan(...any) error
}

func scanInvitation(row rowScanner, now time.Time) (entity.WorkspaceInvitation, error) {
	var invitation entity.WorkspaceInvitation
	err := row.Scan(&invitation.ID, &invitation.WorkspaceID, &invitation.Email, &invitation.Role, &invitation.CreatedBy, &invitation.CreatedAt, &invitation.ExpiresAt, &invitation.RevokedAt, &invitation.RevokedBy, &invitation.AcceptedAt, &invitation.AcceptedBy, &invitation.WorkspaceName, &invitation.WorkspaceSlug)
	if err == nil {
		invitation.SetStatus(now)
	}
	return invitation, err
}

func (r WorkspaceRepository) CreateInvitation(ctx context.Context, invitation entity.WorkspaceInvitation) error {
	return r.WithinTransaction(ctx, func(txCtx context.Context) error {
		if _, err := r.exec(txCtx, `UPDATE workspace_invitations SET revoked_at=$3,revoked_by=$4 WHERE workspace_id=$1 AND email=$2 AND revoked_at IS NULL AND accepted_at IS NULL AND expires_at<=$3`, invitation.WorkspaceID, invitation.Email, invitation.CreatedAt, invitation.CreatedBy); err != nil {
			return err
		}
		_, err := r.exec(txCtx, `INSERT INTO workspace_invitations(id,workspace_id,email,role,created_by,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, invitation.ID, invitation.WorkspaceID, invitation.Email, invitation.Role, invitation.CreatedBy, invitation.CreatedAt, invitation.ExpiresAt)
		return conflictOnUnique(err, "pending invitation already exists")
	})
}

func (r WorkspaceRepository) Invitation(ctx context.Context, workspaceID, invitationID string) (entity.WorkspaceInvitation, error) {
	invitation, err := scanInvitation(r.queryRow(ctx, `SELECT `+invitationColumns+` FROM workspace_invitations i JOIN workspaces w ON w.id=i.workspace_id WHERE i.workspace_id=$1 AND i.id=$2`, workspaceID, invitationID), time.Now().UTC())
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.WorkspaceInvitation{}, repo.NotFound("invitation")
	}
	return invitation, err
}

func (r WorkspaceRepository) InvitationForEmail(ctx context.Context, invitationID, email string) (entity.WorkspaceInvitation, error) {
	invitation, err := scanInvitation(r.queryRow(ctx, `SELECT `+invitationColumns+` FROM workspace_invitations i JOIN workspaces w ON w.id=i.workspace_id WHERE i.id=$1 AND i.email=$2`, invitationID, email), time.Now().UTC())
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.WorkspaceInvitation{}, repo.NotFound("invitation")
	}
	return invitation, err
}

func (r WorkspaceRepository) InvitationsForWorkspacePage(ctx context.Context, workspaceID string, request repo.PageRequest) (repo.Page[entity.WorkspaceInvitation], error) {
	var page repo.Page[entity.WorkspaceInvitation]
	if err := r.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspace_invitations WHERE workspace_id=$1`, workspaceID).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := r.Pool.Query(ctx, `SELECT `+invitationColumns+` FROM workspace_invitations i JOIN workspaces w ON w.id=i.workspace_id WHERE i.workspace_id=$1 ORDER BY i.created_at DESC,i.id LIMIT $2 OFFSET $3`, workspaceID, request.Limit, request.Offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	page.Items = make([]entity.WorkspaceInvitation, 0, request.Limit)
	now := time.Now().UTC()
	for rows.Next() {
		invitation, scanErr := scanInvitation(rows, now)
		if scanErr != nil {
			return repo.Page[entity.WorkspaceInvitation]{}, scanErr
		}
		page.Items = append(page.Items, invitation)
	}
	return page, rows.Err()
}

func (r WorkspaceRepository) InvitationsForEmailPage(ctx context.Context, email string, request repo.PageRequest) (repo.Page[entity.WorkspaceInvitation], error) {
	now := time.Now().UTC()
	var page repo.Page[entity.WorkspaceInvitation]
	if err := r.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspace_invitations WHERE email=$1 AND revoked_at IS NULL AND accepted_at IS NULL AND expires_at>$2`, email, now).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := r.Pool.Query(ctx, `SELECT `+invitationColumns+` FROM workspace_invitations i JOIN workspaces w ON w.id=i.workspace_id WHERE i.email=$1 AND i.revoked_at IS NULL AND i.accepted_at IS NULL AND i.expires_at>$2 ORDER BY i.created_at DESC,i.id LIMIT $3 OFFSET $4`, email, now, request.Limit, request.Offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	page.Items = make([]entity.WorkspaceInvitation, 0, request.Limit)
	for rows.Next() {
		invitation, scanErr := scanInvitation(rows, now)
		if scanErr != nil {
			return repo.Page[entity.WorkspaceInvitation]{}, scanErr
		}
		page.Items = append(page.Items, invitation)
	}
	return page, rows.Err()
}

func (r WorkspaceRepository) RevokeInvitation(ctx context.Context, workspaceID, invitationID, revokedBy string, revokedAt time.Time) (entity.WorkspaceInvitation, error) {
	result, err := r.exec(ctx, `UPDATE workspace_invitations SET revoked_at=$3,revoked_by=$4 WHERE workspace_id=$1 AND id=$2 AND revoked_at IS NULL AND accepted_at IS NULL AND expires_at>$3`, workspaceID, invitationID, revokedAt, revokedBy)
	if err != nil {
		return entity.WorkspaceInvitation{}, err
	}
	if result.RowsAffected() == 0 {
		if _, findErr := r.Invitation(ctx, workspaceID, invitationID); errors.Is(findErr, repo.ErrNotFound) {
			return entity.WorkspaceInvitation{}, findErr
		} else if findErr != nil {
			return entity.WorkspaceInvitation{}, findErr
		}
		return entity.WorkspaceInvitation{}, repo.Conflict("invitation is not pending")
	}
	return r.Invitation(ctx, workspaceID, invitationID)
}

func (r WorkspaceRepository) ClaimInvitation(ctx context.Context, expected entity.WorkspaceInvitation, user entity.User, acceptedAt time.Time) (entity.WorkspaceInvitation, error) {
	err := r.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := lockAccount(txCtx, r.Store, user.ID); err != nil {
			return err
		}
		var role entity.Role
		var workspaceID string
		err := r.queryRow(txCtx, `SELECT workspace_id,role FROM workspace_invitations WHERE id=$1 AND email=$2 AND revoked_at IS NULL AND accepted_at IS NULL AND expires_at>$3 FOR UPDATE`, expected.ID, expected.Email, acceptedAt).Scan(&workspaceID, &role)
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.Conflict("invitation is not pending")
		}
		if err != nil {
			return err
		}
		if workspaceID != expected.WorkspaceID || role != expected.Role {
			return repo.Conflict("invitation changed")
		}
		if err = upsertVerifiedUser(txCtx, r.Store, user); err != nil {
			return err
		}
		if _, err = r.exec(txCtx, `INSERT INTO memberships(workspace_id,user_id,role,created_at) VALUES($1,$2,$3,$4)`, workspaceID, user.ID, role, acceptedAt); err != nil {
			return conflictOnUnique(err, "membership already exists")
		}
		result, err := r.exec(txCtx, `UPDATE workspace_invitations SET accepted_at=$3,accepted_by=$4 WHERE id=$1 AND email=$2 AND revoked_at IS NULL AND accepted_at IS NULL AND expires_at>$3`, expected.ID, expected.Email, acceptedAt, user.ID)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return repo.Conflict("invitation is not pending")
		}
		return nil
	})
	if err != nil {
		return entity.WorkspaceInvitation{}, err
	}
	return r.InvitationForEmail(ctx, expected.ID, expected.Email)
}
func (r WorkspaceRepository) DeleteWorkspace(ctx context.Context, workspaceID string) error {
	return r.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := ensureDeletionNotHeld(txCtx, r.Store, entity.LegalHoldTargetWorkspace, workspaceID); err != nil {
			return err
		}
		result, err := r.exec(txCtx, `DELETE FROM workspaces WHERE id=$1`, workspaceID)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return repo.NotFound("workspace")
		}
		return nil
	})
}
func (r WorkspaceRepository) CurrentPolicy(ctx context.Context, workspaceID string) (entity.Policy, error) {
	var policy entity.Policy
	var raw []byte
	err := r.Pool.QueryRow(ctx, `SELECT id,revision,document_json,created_at FROM policies WHERE workspace_id=$1 ORDER BY revision DESC LIMIT 1`, workspaceID).Scan(&policy.ID, &policy.Revision, &raw, &policy.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Policy{WorkspaceID: workspaceID, Revision: 0, Document: map[string]any{}}, nil
	}
	if err != nil {
		return entity.Policy{}, err
	}
	if err := json.Unmarshal(raw, &policy.Document); err != nil {
		return entity.Policy{}, err
	}
	policy.WorkspaceID = workspaceID
	return policy, nil
}
func (r WorkspaceRepository) SavePolicy(ctx context.Context, policy entity.Policy) error {
	raw, err := json.Marshal(policy.Document)
	if err != nil {
		return err
	}
	_, err = r.exec(ctx, `INSERT INTO policies(id,workspace_id,revision,document_json,created_at) VALUES($1,$2,$3,$4,$5)`, policy.ID, policy.WorkspaceID, policy.Revision, raw, policy.CreatedAt)
	return err
}

func (r WorkspaceRepository) CurrentManifest(ctx context.Context, workspaceID string) (entity.TeamManifest, error) {
	var manifest entity.TeamManifest
	var raw []byte
	err := r.Pool.QueryRow(ctx, `SELECT id,revision,document_json,created_at FROM manifests WHERE workspace_id=$1 ORDER BY revision DESC LIMIT 1`, workspaceID).Scan(&manifest.ID, &manifest.Revision, &raw, &manifest.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.TeamManifest{WorkspaceID: workspaceID, Revision: 0, Document: map[string]any{"version": 1, "packages": []any{}}}, nil
	}
	if err != nil {
		return entity.TeamManifest{}, err
	}
	if err := json.Unmarshal(raw, &manifest.Document); err != nil {
		return entity.TeamManifest{}, err
	}
	manifest.WorkspaceID = workspaceID
	return manifest, nil
}

func (r WorkspaceRepository) SaveManifest(ctx context.Context, manifest entity.TeamManifest) error {
	raw, err := json.Marshal(manifest.Document)
	if err != nil {
		return err
	}
	_, err = r.exec(ctx, `INSERT INTO manifests(id,workspace_id,revision,document_json,created_at) VALUES($1,$2,$3,$4,$5)`, manifest.ID, manifest.WorkspaceID, manifest.Revision, raw, manifest.CreatedAt)
	return err
}
func ensureUser(ctx context.Context, store *Store, userID string) error {
	parts := strings.SplitN(userID, "|", 2)
	issuer, subject := userID, userID
	if len(parts) == 2 {
		issuer, subject = parts[0], parts[1]
	}
	_, err := store.exec(ctx, `INSERT INTO users(id,issuer,subject) VALUES($1,$2,$3) ON CONFLICT(id) DO NOTHING`, userID, issuer, subject)
	return err
}

func upsertVerifiedUser(ctx context.Context, store *Store, user entity.User) error {
	_, err := store.exec(ctx, `INSERT INTO users(id,issuer,subject,email,email_verified,created_at) VALUES($1,$2,$3,$4,true,$5)
		ON CONFLICT(id) DO UPDATE SET issuer=EXCLUDED.issuer,subject=EXCLUDED.subject,email=EXCLUDED.email,email_verified=true`, user.ID, user.Issuer, user.Subject, user.Email, user.CreatedAt)
	return err
}

type PackageRepository struct {
	*Store
	WorkspaceID string
}

func (r PackageRepository) ListForWorkspace(ctx context.Context, wid string) ([]entity.Release, error) {
	rr := r
	rr.WorkspaceID = wid
	return rr.List(ctx)
}
func (r PackageRepository) ListPageForWorkspace(ctx context.Context, wid string, request repo.PageRequest) (repo.Page[entity.Release], error) {
	rr := r
	rr.WorkspaceID = wid
	return rr.ListPage(ctx, request)
}
func (r PackageRepository) SaveForWorkspace(ctx context.Context, wid string, v entity.Release) error {
	rr := r
	rr.WorkspaceID = wid
	return rr.Save(ctx, v)
}
func (r PackageRepository) DigestsForWorkspace(ctx context.Context, workspaceID string) ([]string, error) {
	rows, err := r.query(ctx, `SELECT DISTINCT a.object_key FROM artifacts a JOIN releases rel ON rel.id=a.release_id JOIN packages p ON p.id=rel.package_id WHERE p.workspace_id=$1 ORDER BY a.object_key`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	digests := make([]string, 0)
	for rows.Next() {
		var digest string
		if err := rows.Scan(&digest); err != nil {
			return nil, err
		}
		digests = append(digests, digest)
	}
	return digests, rows.Err()
}
func (r PackageRepository) RemoveWorkspaceReleases(ctx context.Context, workspaceID string) error {
	_, err := r.exec(ctx, `DELETE FROM packages WHERE workspace_id=$1`, workspaceID)
	return err
}
func (r PackageRepository) ArtifactReferenced(ctx context.Context, digest string) (bool, error) {
	var found bool
	err := r.queryRow(ctx, `SELECT EXISTS(SELECT 1 FROM artifacts WHERE object_key=$1)`, digest).Scan(&found)
	return found, err
}
func (r PackageRepository) WithArtifactReferenceLock(ctx context.Context, digest string, operation func(context.Context) error) (resultErr error) {
	connection, err := r.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer connection.Release()
	lockKey := "agentx-artifact\n" + digest
	if _, err = connection.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, lockKey); err != nil {
		return err
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, unlockErr := connection.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, lockKey); unlockErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("unlock artifact reference: %w", unlockErr))
		}
	}()
	return operation(ctx)
}
func (r PackageRepository) HasArtifactForWorkspace(ctx context.Context, workspaceID, digest string) (bool, error) {
	var found bool
	err := r.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM artifacts a JOIN releases r ON r.id=a.release_id JOIN packages p ON p.id=r.package_id WHERE p.workspace_id=$1 AND a.object_key=$2)`, workspaceID, digest).Scan(&found)
	return found, err
}
func (r PackageRepository) FindReleaseByDigestForWorkspace(ctx context.Context, workspaceID, digest string) (entity.Release, error) {
	var release entity.Release
	var metadata []byte
	err := r.Pool.QueryRow(ctx, `SELECT p.name,r.version,r.sha256,r.status,r.metadata_json,r.created_at,COALESCE(a.size_bytes,0) FROM packages p JOIN releases r ON r.package_id=p.id JOIN artifacts a ON a.release_id=r.id WHERE p.workspace_id=$1 AND a.object_key=$2`, workspaceID, digest).Scan(&release.Name, &release.Version, &release.SHA256, &release.Status, &metadata, &release.CreatedAt, &release.Size)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Release{}, repo.NotFound("artifact")
	}
	if err != nil {
		return entity.Release{}, err
	}
	if err := applyReleaseMetadata(&release, metadata); err != nil {
		return entity.Release{}, err
	}
	return release, nil
}
func (r PackageRepository) FindReleaseForWorkspace(ctx context.Context, workspaceID, name, version string) (entity.Release, error) {
	var release entity.Release
	err := r.queryRow(ctx, `SELECT p.name,r.version,r.sha256,r.status,r.created_at,COALESCE(a.size_bytes,0) FROM packages p JOIN releases r ON r.package_id=p.id LEFT JOIN artifacts a ON a.release_id=r.id WHERE p.workspace_id=$1 AND p.name=$2 AND r.version=$3`, workspaceID, name, version).Scan(&release.Name, &release.Version, &release.SHA256, &release.Status, &release.CreatedAt, &release.Size)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Release{}, repo.NotFound("release")
	}
	return release, err
}
func (r PackageRepository) ApproveReleaseForWorkspace(ctx context.Context, workspaceID, name, version string) (entity.Release, error) {
	var release entity.Release
	err := r.queryRow(ctx, `UPDATE releases r SET status='approved' FROM packages p WHERE r.package_id=p.id AND p.workspace_id=$1 AND p.name=$2 AND r.version=$3 AND r.status='pending_approval' RETURNING p.name,r.version,r.sha256,r.status,r.created_at`, workspaceID, name, version).Scan(&release.Name, &release.Version, &release.SHA256, &release.Status, &release.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, findErr := r.FindReleaseForWorkspace(ctx, workspaceID, name, version); findErr == nil {
			return entity.Release{}, repo.Conflict("release is not pending approval")
		} else if errors.Is(findErr, repo.ErrNotFound) {
			return entity.Release{}, repo.NotFound("release")
		} else {
			return entity.Release{}, findErr
		}
	}
	return release, err
}
func (r PackageRepository) LookupIdempotency(ctx context.Context, workspaceID, key string) (entity.IdempotencyRecord, bool, error) {
	var raw []byte
	var fingerprint, resourceType string
	err := r.Pool.QueryRow(ctx, `SELECT resource_type,request_fingerprint,response_json FROM idempotency_keys WHERE workspace_id=$1 AND key=$2`, workspaceID, key).Scan(&resourceType, &fingerprint, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.IdempotencyRecord{}, false, nil
	}
	if err != nil {
		return entity.IdempotencyRecord{}, false, err
	}
	if resourceType != "release" {
		return entity.IdempotencyRecord{}, false, repo.IdempotencyConflict("idempotency key already used for a different operation")
	}
	var v entity.Release
	if err = json.Unmarshal(raw, &v); err != nil {
		return entity.IdempotencyRecord{}, false, err
	}
	return entity.IdempotencyRecord{Fingerprint: fingerprint, Release: v}, true, nil
}
func (r PackageRepository) StoreIdempotency(ctx context.Context, workspaceID, key string, v entity.IdempotencyRecord) error {
	raw, err := json.Marshal(v.Release)
	if err != nil {
		return err
	}
	_, err = r.Pool.Exec(ctx, `INSERT INTO idempotency_keys(workspace_id,key,resource_type,request_fingerprint,response_json) VALUES($1,$2,'release',$3,$4) ON CONFLICT(workspace_id,key) DO NOTHING`, workspaceID, key, v.Fingerprint, raw)
	return err
}

func (r PackageRepository) SaveForWorkspaceIdempotent(ctx context.Context, workspaceID, key string, record entity.IdempotencyRecord, audit entity.AuditEvent, outbox entity.OutboxEvent) (entity.Release, bool, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return entity.Release{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, workspaceID+"\n"+key); err != nil {
		return entity.Release{}, false, err
	}
	var fingerprint, resourceType string
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT resource_type,request_fingerprint,response_json FROM idempotency_keys WHERE workspace_id=$1 AND key=$2`, workspaceID, key).Scan(&resourceType, &fingerprint, &raw)
	if err == nil {
		if resourceType != "release" {
			return entity.Release{}, false, repo.IdempotencyConflict("idempotency key already used for a different operation")
		}
		var existing entity.Release
		if err = json.Unmarshal(raw, &existing); err != nil {
			return entity.Release{}, false, err
		}
		if fingerprint != record.Fingerprint {
			return entity.Release{}, false, repo.IdempotencyConflict("idempotency key already used for a different request")
		}
		return existing, true, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return entity.Release{}, false, err
	}
	if err = saveReleaseTx(ctx, tx, workspaceID, record.Release); err != nil {
		return entity.Release{}, false, err
	}
	raw, err = json.Marshal(record.Release)
	if err != nil {
		return entity.Release{}, false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO idempotency_keys(workspace_id,key,resource_type,request_fingerprint,response_json) VALUES($1,$2,'release',$3,$4)`, workspaceID, key, record.Fingerprint, raw); err != nil {
		return entity.Release{}, false, err
	}
	auditMetadata, err := json.Marshal(audit.Metadata)
	if err != nil {
		return entity.Release{}, false, err
	}
	if string(auditMetadata) == "null" {
		auditMetadata = []byte("{}")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(workspace_id,actor_id,action,resource_type,resource_id,metadata_json,request_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, workspaceID, audit.ActorID, audit.Action, audit.ResourceType, audit.ResourceID, auditMetadata, audit.RequestID, audit.At); err != nil {
		return entity.Release{}, false, err
	}
	outboxPayload, err := json.Marshal(outbox.Payload)
	if err != nil {
		return entity.Release{}, false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO outbox(id,topic,payload_json,attempts,available_at) VALUES($1,$2,$3,$4,$5)`, outbox.ID, outbox.Topic, outboxPayload, outbox.Attempts, outbox.AvailableAt); err != nil {
		return entity.Release{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return entity.Release{}, false, err
	}
	return record.Release, false, nil
}

func (r PackageRepository) List(ctx context.Context) ([]entity.Release, error) {
	rows, err := r.Pool.Query(ctx, `SELECT p.name,r.version,r.sha256,r.status,r.metadata_json,r.created_at,COALESCE(a.size_bytes,0) FROM packages p JOIN releases r ON r.package_id=p.id LEFT JOIN artifacts a ON a.release_id=r.id WHERE p.workspace_id=$1 ORDER BY p.name,r.created_at DESC`, r.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entity.Release
	for rows.Next() {
		var v entity.Release
		var metadata []byte
		if err := rows.Scan(&v.Name, &v.Version, &v.SHA256, &v.Status, &metadata, &v.CreatedAt, &v.Size); err != nil {
			return nil, err
		}
		if err := applyReleaseMetadata(&v, metadata); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r PackageRepository) ListPage(ctx context.Context, request repo.PageRequest) (repo.Page[entity.Release], error) {
	var page repo.Page[entity.Release]
	if err := r.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM releases r JOIN packages p ON p.id=r.package_id WHERE p.workspace_id=$1`, r.WorkspaceID).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := r.Pool.Query(ctx, `SELECT p.name,r.version,r.sha256,r.status,r.metadata_json,r.created_at,COALESCE(a.size_bytes,0) FROM packages p JOIN releases r ON r.package_id=p.id LEFT JOIN artifacts a ON a.release_id=r.id WHERE p.workspace_id=$1 ORDER BY p.name,r.created_at DESC,r.id LIMIT $2 OFFSET $3`, r.WorkspaceID, request.Limit, request.Offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	page.Items = make([]entity.Release, 0, request.Limit)
	for rows.Next() {
		var release entity.Release
		var metadata []byte
		if err := rows.Scan(&release.Name, &release.Version, &release.SHA256, &release.Status, &metadata, &release.CreatedAt, &release.Size); err != nil {
			return repo.Page[entity.Release]{}, err
		}
		if err := applyReleaseMetadata(&release, metadata); err != nil {
			return repo.Page[entity.Release]{}, err
		}
		page.Items = append(page.Items, release)
	}
	return page, rows.Err()
}
func (r PackageRepository) Save(ctx context.Context, v entity.Release) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = saveReleaseTx(ctx, tx, r.WorkspaceID, v); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func saveReleaseTx(ctx context.Context, tx pgx.Tx, workspaceID string, v entity.Release) error {
	var pid string
	err := tx.QueryRow(ctx, `INSERT INTO packages(workspace_id,name) VALUES($1,$2) ON CONFLICT(workspace_id,name) DO UPDATE SET name=EXCLUDED.name RETURNING id`, workspaceID, v.Name).Scan(&pid)
	if err != nil {
		return err
	}
	var rid string
	metadata, err := releaseMetadata(v)
	if err != nil {
		return err
	}
	if v.Status == "" {
		v.Status = "published"
	}
	err = tx.QueryRow(ctx, `INSERT INTO releases(package_id,version,sha256,metadata_json,status,created_at) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, pid, v.Version, v.SHA256, metadata, v.Status, v.CreatedAt).Scan(&rid)
	if err != nil {
		return conflictOnUnique(err, "release version already exists")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO artifacts(release_id,object_key,size_bytes) VALUES($1,$2,$3)`, rid, v.SHA256, v.Size); err != nil {
		return err
	}
	return nil
}

func releaseMetadata(v entity.Release) ([]byte, error) {
	metadata := map[string]string{}
	if v.Signature != "" {
		metadata["signature"] = v.Signature
	}
	if v.SignatureStatus != "" {
		metadata["signature_status"] = v.SignatureStatus
	}
	return json.Marshal(metadata)
}

func applyReleaseMetadata(v *entity.Release, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	var metadata map[string]string
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return err
	}
	v.Signature = metadata["signature"]
	v.SignatureStatus = metadata["signature_status"]
	return nil
}

type DeviceRepository struct {
	*Store
	WorkspaceID string
}

func (r DeviceRepository) ListForWorkspace(ctx context.Context, wid string) ([]entity.Device, error) {
	rr := r
	rr.WorkspaceID = wid
	return rr.List(ctx)
}
func (r DeviceRepository) ListPageForWorkspace(ctx context.Context, wid string, request repo.PageRequest) (repo.Page[entity.Device], error) {
	rr := r
	rr.WorkspaceID = wid
	return rr.ListPage(ctx, request)
}
func (r DeviceRepository) SaveForWorkspace(ctx context.Context, wid string, d entity.Device) error {
	rr := r
	rr.WorkspaceID = wid
	return rr.Save(ctx, d)
}
func (r DeviceRepository) HeartbeatForWorkspace(ctx context.Context, wid, deviceID string, d entity.Device) error {
	rr := r
	rr.WorkspaceID = wid
	d.ID = deviceID
	return rr.Save(ctx, d)
}

func (r DeviceRepository) SaveForWorkspaceIdempotent(ctx context.Context, workspaceID, key string, record entity.DeviceIdempotencyRecord, audit entity.AuditEvent, outbox entity.OutboxEvent) (entity.Device, bool, error) {
	var saved entity.Device
	var replayed bool
	err := r.WithinTransaction(ctx, func(txCtx context.Context) error {
		if _, err := r.exec(txCtx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, workspaceID+"\n"+key); err != nil {
			return err
		}
		var resourceType, fingerprint string
		var raw []byte
		err := r.queryRow(txCtx, `SELECT resource_type,request_fingerprint,response_json FROM idempotency_keys WHERE workspace_id=$1 AND key=$2`, workspaceID, key).Scan(&resourceType, &fingerprint, &raw)
		if err == nil {
			if resourceType != "device" {
				return repo.IdempotencyConflict("idempotency key already used for a different operation")
			}
			if fingerprint != record.Fingerprint {
				return repo.IdempotencyConflict("idempotency key already used for a different request")
			}
			if err = json.Unmarshal(raw, &saved); err != nil {
				return err
			}
			replayed = true
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err = r.SaveForWorkspace(txCtx, workspaceID, record.Device); err != nil {
			return err
		}
		raw, err = json.Marshal(record.Device)
		if err != nil {
			return err
		}
		if _, err = r.exec(txCtx, `INSERT INTO idempotency_keys(workspace_id,key,resource_type,request_fingerprint,response_json) VALUES($1,$2,'device',$3,$4)`, workspaceID, key, record.Fingerprint, raw); err != nil {
			return err
		}
		if err = (AuditRepository{Store: r.Store}).AppendForWorkspace(txCtx, workspaceID, audit); err != nil {
			return err
		}
		if err = (OutboxRepo{Store: r.Store}).Enqueue(txCtx, outbox); err != nil {
			return err
		}
		saved = record.Device
		return nil
	})
	return saved, replayed, err
}

func conflictOnUnique(err error, message string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return repo.Conflict(message)
	}
	return err
}

func (r DeviceRepository) List(ctx context.Context) ([]entity.Device, error) {
	rows, err := r.Pool.Query(ctx, `SELECT id,name,COALESCE(agent_versions_json->>'agent',''),COALESCE(agent_versions_json->>'status','offline'),COALESCE(last_seen_at,now()) FROM devices WHERE workspace_id=$1 ORDER BY name`, r.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entity.Device
	for rows.Next() {
		var d entity.Device
		if err := rows.Scan(&d.ID, &d.Name, &d.Agent, &d.Status, &d.UpdatedAt); err != nil {
			return nil, err
		}
		d.InstalledPackages, err = r.installedPackages(ctx, d.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (r DeviceRepository) ListPage(ctx context.Context, request repo.PageRequest) (repo.Page[entity.Device], error) {
	var page repo.Page[entity.Device]
	if err := r.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices WHERE workspace_id=$1`, r.WorkspaceID).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := r.Pool.Query(ctx, `SELECT id,name,COALESCE(agent_versions_json->>'agent',''),COALESCE(agent_versions_json->>'status','offline'),COALESCE(last_seen_at,now()) FROM devices WHERE workspace_id=$1 ORDER BY name,id LIMIT $2 OFFSET $3`, r.WorkspaceID, request.Limit, request.Offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	page.Items = make([]entity.Device, 0, request.Limit)
	for rows.Next() {
		var device entity.Device
		if err := rows.Scan(&device.ID, &device.Name, &device.Agent, &device.Status, &device.UpdatedAt); err != nil {
			return repo.Page[entity.Device]{}, err
		}
		device.InstalledPackages, err = r.installedPackages(ctx, device.ID)
		if err != nil {
			return repo.Page[entity.Device]{}, err
		}
		page.Items = append(page.Items, device)
	}
	return page, rows.Err()
}
func (r DeviceRepository) Save(ctx context.Context, d entity.Device) error {
	return r.WithinTransaction(ctx, func(txCtx context.Context) error {
		doc, _ := json.Marshal(map[string]string{"agent": d.Agent, "status": d.Status})
		result, err := r.exec(txCtx, `INSERT INTO devices(id,workspace_id,name,agent_versions_json,last_seen_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(workspace_id,id) DO UPDATE SET name=EXCLUDED.name,agent_versions_json=EXCLUDED.agent_versions_json,last_seen_at=EXCLUDED.last_seen_at`, d.ID, r.WorkspaceID, d.Name, doc, d.UpdatedAt)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return fmt.Errorf("device %s does not belong to workspace", d.ID)
		}
		if d.InstalledPackages != nil {
			if _, err = r.exec(txCtx, `DELETE FROM device_packages WHERE workspace_id=$1 AND device_id=$2`, r.WorkspaceID, d.ID); err != nil {
				return err
			}
			for name, digest := range d.InstalledPackages {
				if _, err = r.exec(txCtx, `INSERT INTO device_packages(workspace_id,device_id,name,version,sha256,observed_at) VALUES($1,$2,$3,$4,$5,$6)`, r.WorkspaceID, d.ID, name, "", digest, d.UpdatedAt); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (r DeviceRepository) installedPackages(ctx context.Context, deviceID string) (map[string]string, error) {
	rows, err := r.Pool.Query(ctx, `SELECT name,sha256 FROM device_packages WHERE workspace_id=$1 AND device_id=$2`, r.WorkspaceID, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	packages := map[string]string{}
	for rows.Next() {
		var name, digest string
		if err := rows.Scan(&name, &digest); err != nil {
			return nil, err
		}
		packages[name] = digest
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(packages) == 0 {
		return nil, nil
	}
	return packages, nil
}

type AuditRepository struct {
	*Store
	WorkspaceID string
}

func (r AuditRepository) ListForWorkspace(ctx context.Context, wid string) ([]entity.AuditEvent, error) {
	rr := r
	rr.WorkspaceID = wid
	return rr.List(ctx)
}
func (r AuditRepository) ListPageForWorkspace(ctx context.Context, wid string, request repo.PageRequest) (repo.Page[entity.AuditEvent], error) {
	rr := r
	rr.WorkspaceID = wid
	return rr.ListPage(ctx, request)
}

func (r AuditRepository) List(ctx context.Context) ([]entity.AuditEvent, error) {
	rows, err := r.Pool.Query(ctx, `SELECT action,COALESCE(actor_id,''),COALESCE(resource_id,''),COALESCE(resource_type,''),metadata_json,COALESCE(request_id,''),created_at FROM audit_events WHERE workspace_id=$1 ORDER BY created_at DESC`, r.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entity.AuditEvent
	for rows.Next() {
		var e entity.AuditEvent
		var raw []byte
		if err := rows.Scan(&e.Action, &e.ActorID, &e.ResourceID, &e.ResourceType, &raw, &e.RequestID, &e.At); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &e.Metadata); err != nil {
				return nil, err
			}
		}
		if e.ResourceType == "device" {
			e.DeviceID = e.ResourceID
		}
		if e.ResourceID == "" && e.DeviceID != "" {
			e.ResourceID = e.DeviceID
		}
		if e.ResourceType == "" && e.DeviceID != "" {
			e.ResourceType = "device"
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (r AuditRepository) ListPage(ctx context.Context, request repo.PageRequest) (repo.Page[entity.AuditEvent], error) {
	var page repo.Page[entity.AuditEvent]
	if err := r.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_events WHERE workspace_id=$1`, r.WorkspaceID).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := r.Pool.Query(ctx, `SELECT action,COALESCE(actor_id,''),COALESCE(resource_id,''),COALESCE(resource_type,''),metadata_json,COALESCE(request_id,''),created_at FROM audit_events WHERE workspace_id=$1 ORDER BY created_at DESC,id LIMIT $2 OFFSET $3`, r.WorkspaceID, request.Limit, request.Offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	page.Items = make([]entity.AuditEvent, 0, request.Limit)
	for rows.Next() {
		var event entity.AuditEvent
		var raw []byte
		if err := rows.Scan(&event.Action, &event.ActorID, &event.ResourceID, &event.ResourceType, &raw, &event.RequestID, &event.At); err != nil {
			return repo.Page[entity.AuditEvent]{}, err
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &event.Metadata); err != nil {
				return repo.Page[entity.AuditEvent]{}, err
			}
		}
		if event.ResourceType == "device" {
			event.DeviceID = event.ResourceID
		}
		page.Items = append(page.Items, event)
	}
	return page, rows.Err()
}
func (r AuditRepository) Append(ctx context.Context, e entity.AuditEvent) error {
	resourceID := e.ResourceID
	if resourceID == "" {
		resourceID = e.DeviceID
	}
	metadata, err := json.Marshal(e.Metadata)
	if err != nil {
		return err
	}
	_, err = r.exec(ctx, `INSERT INTO audit_events(workspace_id,actor_id,action,resource_type,resource_id,metadata_json,request_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, r.WorkspaceID, e.ActorID, e.Action, e.ResourceType, resourceID, metadata, e.RequestID, e.At)
	return err
}
func (r AuditRepository) AppendForWorkspace(ctx context.Context, workspaceID string, e entity.AuditEvent) error {
	resourceID := e.ResourceID
	if resourceID == "" {
		resourceID = e.DeviceID
	}
	metadata, err := json.Marshal(e.Metadata)
	if err != nil {
		return err
	}
	_, err = r.exec(ctx, `INSERT INTO audit_events(workspace_id,actor_id,action,resource_type,resource_id,metadata_json,request_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, workspaceID, e.ActorID, e.Action, e.ResourceType, resourceID, metadata, e.RequestID, e.At)
	return err
}
