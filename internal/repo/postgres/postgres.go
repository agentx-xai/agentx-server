package postgres

import (
	"agentx/server/internal/entity"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
)

type Store struct{ Pool *pgxpool.Pool }

func Open(ctx context.Context, url string) (*Store, error) {
	p, err := pgxpool.New(ctx, url)
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
func (r WorkspaceRepository) Create(ctx context.Context, w entity.Workspace, m entity.Membership) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = ensureUser(ctx, tx, m.UserID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO workspaces(id,slug,name,created_at) VALUES($1,$2,$3,$4)`, w.ID, w.Slug, w.Name, w.CreatedAt); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO memberships(workspace_id,user_id,role,created_at) VALUES($1,$2,$3,$4)`, m.WorkspaceID, m.UserID, m.Role, m.CreatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r WorkspaceRepository) Membership(ctx context.Context, wid, uid string) (entity.Membership, error) {
	var m entity.Membership
	err := r.Pool.QueryRow(ctx, `SELECT workspace_id,user_id,role,created_at FROM memberships WHERE workspace_id=$1 AND user_id=$2`, wid, uid).Scan(&m.WorkspaceID, &m.UserID, &m.Role, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, errors.New("membership not found")
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
func (r WorkspaceRepository) AddMember(ctx context.Context, m entity.Membership) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = ensureUser(ctx, tx, m.UserID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO memberships(workspace_id,user_id,role,created_at) VALUES($1,$2,$3,$4)`, m.WorkspaceID, m.UserID, m.Role, m.CreatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r WorkspaceRepository) UpdateMemberRole(ctx context.Context, workspaceID, userID string, role entity.Role) error {
	result, err := r.Pool.Exec(ctx, `UPDATE memberships SET role=$3 WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID, role)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errors.New("membership not found")
	}
	return nil
}
func (r WorkspaceRepository) RemoveMember(ctx context.Context, workspaceID, userID string) error {
	result, err := r.Pool.Exec(ctx, `DELETE FROM memberships WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errors.New("membership not found")
	}
	return nil
}
func (r WorkspaceRepository) DeleteWorkspace(ctx context.Context, workspaceID string) error {
	result, err := r.Pool.Exec(ctx, `DELETE FROM workspaces WHERE id=$1`, workspaceID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errors.New("workspace not found")
	}
	return nil
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
	_, err = r.Pool.Exec(ctx, `INSERT INTO policies(id,workspace_id,revision,document_json,created_at) VALUES($1,$2,$3,$4,$5)`, policy.ID, policy.WorkspaceID, policy.Revision, raw, policy.CreatedAt)
	return err
}

func (r WorkspaceRepository) CurrentManifest(ctx context.Context, workspaceID string) (entity.TeamManifest, error) {
	var manifest entity.TeamManifest
	var raw []byte
	err := r.Pool.QueryRow(ctx, `SELECT id,revision,document_json,created_at FROM manifests WHERE workspace_id=$1 ORDER BY revision DESC LIMIT 1`, workspaceID).Scan(&manifest.ID, &manifest.Revision, &raw, &manifest.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.TeamManifest{WorkspaceID: workspaceID, Revision: 0, Document: map[string]any{}}, nil
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
	_, err = r.Pool.Exec(ctx, `INSERT INTO manifests(id,workspace_id,revision,document_json,created_at) VALUES($1,$2,$3,$4,$5)`, manifest.ID, manifest.WorkspaceID, manifest.Revision, raw, manifest.CreatedAt)
	return err
}
func ensureUser(ctx context.Context, tx pgx.Tx, userID string) error {
	parts := strings.SplitN(userID, "|", 2)
	issuer, subject := userID, userID
	if len(parts) == 2 {
		issuer, subject = parts[0], parts[1]
	}
	_, err := tx.Exec(ctx, `INSERT INTO users(id,issuer,subject) VALUES($1,$2,$3) ON CONFLICT(id) DO NOTHING`, userID, issuer, subject)
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
func (r PackageRepository) SaveForWorkspace(ctx context.Context, wid string, v entity.Release) error {
	rr := r
	rr.WorkspaceID = wid
	return rr.Save(ctx, v)
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
		return entity.Release{}, errors.New("artifact not found")
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
	err := r.Pool.QueryRow(ctx, `SELECT p.name,r.version,r.sha256,r.status,r.created_at,COALESCE(a.size_bytes,0) FROM packages p JOIN releases r ON r.package_id=p.id LEFT JOIN artifacts a ON a.release_id=r.id WHERE p.workspace_id=$1 AND p.name=$2 AND r.version=$3`, workspaceID, name, version).Scan(&release.Name, &release.Version, &release.SHA256, &release.Status, &release.CreatedAt, &release.Size)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Release{}, errors.New("release not found")
	}
	return release, err
}
func (r PackageRepository) ApproveReleaseForWorkspace(ctx context.Context, workspaceID, name, version string) (entity.Release, error) {
	var release entity.Release
	err := r.Pool.QueryRow(ctx, `UPDATE releases r SET status='approved' FROM packages p WHERE r.package_id=p.id AND p.workspace_id=$1 AND p.name=$2 AND r.version=$3 RETURNING p.name,r.version,r.sha256,r.status,r.created_at`, workspaceID, name, version).Scan(&release.Name, &release.Version, &release.SHA256, &release.Status, &release.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Release{}, errors.New("release not found")
	}
	return release, err
}
func (r PackageRepository) LookupIdempotency(ctx context.Context, workspaceID, key string) (entity.Release, bool, error) {
	var raw []byte
	err := r.Pool.QueryRow(ctx, `SELECT response_json FROM idempotency_keys WHERE workspace_id=$1 AND key=$2`, workspaceID, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Release{}, false, nil
	}
	if err != nil {
		return entity.Release{}, false, err
	}
	var v entity.Release
	if err = json.Unmarshal(raw, &v); err != nil {
		return entity.Release{}, false, err
	}
	return v, true, nil
}
func (r PackageRepository) StoreIdempotency(ctx context.Context, workspaceID, key string, v entity.Release) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = r.Pool.Exec(ctx, `INSERT INTO idempotency_keys(workspace_id,key,response_json) VALUES($1,$2,$3) ON CONFLICT(workspace_id,key) DO NOTHING`, workspaceID, key, raw)
	return err
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
func (r PackageRepository) Save(ctx context.Context, v entity.Release) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var pid string
	err = tx.QueryRow(ctx, `INSERT INTO packages(workspace_id,name) VALUES($1,$2) ON CONFLICT(workspace_id,name) DO UPDATE SET name=EXCLUDED.name RETURNING id`, r.WorkspaceID, v.Name).Scan(&pid)
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
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO artifacts(release_id,object_key,size_bytes) VALUES($1,$2,$3)`, rid, v.SHA256, v.Size); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
func (r DeviceRepository) Save(ctx context.Context, d entity.Device) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	doc, _ := json.Marshal(map[string]string{"agent": d.Agent, "status": d.Status})
	result, err := tx.Exec(ctx, `INSERT INTO devices(id,workspace_id,name,agent_versions_json,last_seen_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,agent_versions_json=EXCLUDED.agent_versions_json,last_seen_at=EXCLUDED.last_seen_at WHERE devices.workspace_id=EXCLUDED.workspace_id`, d.ID, r.WorkspaceID, d.Name, doc, d.UpdatedAt)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("device %s does not belong to workspace", d.ID)
	}
	if d.InstalledPackages != nil {
		if _, err = tx.Exec(ctx, `DELETE FROM device_packages WHERE device_id=$1`, d.ID); err != nil {
			return err
		}
		for name, digest := range d.InstalledPackages {
			if _, err = tx.Exec(ctx, `INSERT INTO device_packages(device_id,name,version,sha256,observed_at) VALUES($1,$2,$3,$4,$5)`, d.ID, name, "", digest, d.UpdatedAt); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

func (r DeviceRepository) installedPackages(ctx context.Context, deviceID string) (map[string]string, error) {
	rows, err := r.Pool.Query(ctx, `SELECT name,sha256 FROM device_packages WHERE device_id=$1`, deviceID)
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
func (r AuditRepository) Append(ctx context.Context, e entity.AuditEvent) error {
	resourceID := e.ResourceID
	if resourceID == "" {
		resourceID = e.DeviceID
	}
	metadata, err := json.Marshal(e.Metadata)
	if err != nil {
		return err
	}
	_, err = r.Pool.Exec(ctx, `INSERT INTO audit_events(workspace_id,actor_id,action,resource_type,resource_id,metadata_json,request_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, r.WorkspaceID, e.ActorID, e.Action, e.ResourceType, resourceID, metadata, e.RequestID, e.At)
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
	_, err = r.Pool.Exec(ctx, `INSERT INTO audit_events(workspace_id,actor_id,action,resource_type,resource_id,metadata_json,request_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, workspaceID, e.ActorID, e.Action, e.ResourceType, resourceID, metadata, e.RequestID, e.At)
	return err
}
