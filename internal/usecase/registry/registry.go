package registry

import (
	"agentx/server/internal/auth"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"agentx/server/internal/usecase/workspace"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	store         repo.ArtifactStore
	packages      repo.PackageRepository
	verifier      repo.SignatureVerifier
	audit         repo.WorkspaceAuditWriter
	workspaces    *workspace.Service
	policy        repo.PolicyRepository
	outbox        repo.OutboxRepository
	idempotencyMu sync.Mutex
}

func New(s repo.ArtifactStore, p repo.PackageRepository, v ...repo.SignatureVerifier) *Service {
	var verifier repo.SignatureVerifier
	if len(v) > 0 {
		verifier = v[0]
	}
	return &Service{store: s, packages: p, verifier: verifier}
}
func (s *Service) SetAudit(w repo.WorkspaceAuditWriter) { s.audit = w }
func (s *Service) SetWorkspace(w *workspace.Service)    { s.workspaces = w }
func (s *Service) SetPolicy(p repo.PolicyRepository)    { s.policy = p }
func (s *Service) SetOutbox(w repo.OutboxRepository)    { s.outbox = w }
func auditEvent(ctx context.Context, event entity.AuditEvent) entity.AuditEvent {
	if principal, ok := auth.FromContext(ctx); ok {
		event.ActorID = principal.UserID
	}
	event.RequestID = auth.RequestID(ctx)
	return event
}
func (s *Service) prepare(ctx context.Context, name, version string, r io.Reader, signatures ...string) (entity.Release, error) {
	if strings.TrimSpace(name) == "" || !regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$`).MatchString(strings.TrimSpace(version)) {
		return entity.Release{}, fmt.Errorf("package name and version are required")
	}
	v, err := s.store.Put(ctx, name, r)
	if err != nil {
		return v, err
	}
	v.Version = version
	v.Status = "published"
	if s.verifier != nil {
		if len(signatures) == 0 || signatures[0] == "" {
			return v, fmt.Errorf("artifact signature is required")
		}
		f, err := s.store.Open(ctx, v.SHA256)
		if err != nil {
			return v, err
		}
		defer f.Close()
		data, err := io.ReadAll(f)
		if err != nil {
			return v, err
		}
		if err = s.verifier.Verify(ctx, data, signatures[0]); err != nil {
			_ = s.store.Delete(ctx, v.SHA256)
			return v, err
		}
		v.Signature, v.SignatureStatus = signatures[0], "verified"
	}
	return v, nil
}
func (s *Service) Publish(ctx context.Context, name, version string, r io.Reader, signatures ...string) (entity.Release, error) {
	v, err := s.prepare(ctx, name, version, r, signatures...)
	if err != nil {
		return v, err
	}
	if err = s.packages.Save(ctx, v); err != nil {
		return v, err
	}
	return v, nil
}
func (s *Service) PublishForWorkspace(ctx context.Context, workspaceID, name, version string, r io.Reader, signatures ...string) (entity.Release, error) {
	status, err := s.releaseStatus(ctx, workspaceID, signatures...)
	if err != nil {
		return entity.Release{}, err
	}
	if p, ok := s.packages.(repo.WorkspacePackageRepository); ok {
		v, err := s.prepare(ctx, name, version, r, signatures...)
		if err != nil {
			return v, err
		}
		v.Status = status
		if err = p.SaveForWorkspace(ctx, workspaceID, v); err != nil {
			return v, err
		}
		if s.audit != nil {
			_ = s.audit.AppendForWorkspace(ctx, workspaceID, auditEvent(ctx, entity.AuditEvent{Action: "release.publish", ResourceType: "release", ResourceID: v.Name + "@" + v.Version, At: v.CreatedAt}))
		}
		if err := s.enqueue(ctx, workspaceID, "release.published", map[string]any{"workspace_id": workspaceID, "name": v.Name, "version": v.Version, "sha256": v.SHA256, "status": v.Status}); err != nil {
			if s.audit != nil {
				_ = s.audit.AppendForWorkspace(ctx, workspaceID, auditEvent(ctx, entity.AuditEvent{Action: "outbox.enqueue_failed", ResourceType: "release", ResourceID: v.Name + "@" + v.Version, Metadata: map[string]any{"error": err.Error()}, At: time.Now().UTC()}))
			}
			return v, fmt.Errorf("release committed but outbox enqueue failed: %w", err)
		}
		return v, nil
	}
	return s.Publish(ctx, name, version, r, signatures...)
}

func (s *Service) releaseStatus(ctx context.Context, workspaceID string, signatures ...string) (string, error) {
	status := "published"
	if s.policy == nil {
		return status, nil
	}
	current, err := s.policy.CurrentPolicy(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	if policyBool(current.Document, "require_signature") && (len(signatures) == 0 || strings.TrimSpace(signatures[0]) == "") {
		return "", fmt.Errorf("release signature is required by workspace policy")
	}
	if policyBool(current.Document, "require_signature") && s.verifier == nil {
		return "", fmt.Errorf("artifact signature verification is not configured")
	}
	if policyBool(current.Document, "require_approval") {
		status = "pending_approval"
	}
	return status, nil
}

func policyBool(document map[string]any, key string) bool {
	v, ok := document[key]
	if !ok {
		return false
	}
	switch value := v.(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	default:
		return false
	}
}
func (s *Service) PublishForWorkspaceIdempotent(ctx context.Context, workspaceID, key, name, version string, r io.Reader, signatures ...string) (entity.Release, bool, error) {
	if key == "" {
		v, err := s.PublishForWorkspace(ctx, workspaceID, name, version, r, signatures...)
		return v, false, err
	}
	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	if existing, found, err := s.LookupIdempotency(ctx, workspaceID, key); err != nil {
		return entity.Release{}, false, err
	} else if found {
		return existing, true, nil
	}
	v, err := s.PublishForWorkspace(ctx, workspaceID, name, version, r, signatures...)
	if err != nil {
		return v, false, err
	}
	if err = s.StoreIdempotency(ctx, workspaceID, key, v); err != nil {
		return v, false, err
	}
	return v, false, nil
}
func (s *Service) Open(ctx context.Context, digest string) (io.ReadCloser, error) {
	return s.store.Open(ctx, digest)
}
func (s *Service) OpenForWorkspace(ctx context.Context, workspaceID, digest string) (io.ReadCloser, error) {
	if repository, ok := s.packages.(repo.WorkspaceArtifactRepository); ok {
		release, err := repository.FindReleaseByDigestForWorkspace(ctx, workspaceID, digest)
		if err != nil {
			return nil, err
		}
		if release.Status == "pending_approval" {
			return nil, fmt.Errorf("release is pending approval")
		}
	} else {
		return nil, fmt.Errorf("workspace artifact lookup is unavailable")
	}
	return s.store.Open(ctx, digest)
}
func (s *Service) OpenReleaseForWorkspace(ctx context.Context, workspaceID, name, version string) (io.ReadCloser, error) {
	repository, ok := s.packages.(repo.WorkspaceArtifactRepository)
	if !ok {
		return nil, fmt.Errorf("workspace artifact lookup is unavailable")
	}
	release, err := repository.FindReleaseForWorkspace(ctx, workspaceID, name, version)
	if err != nil {
		return nil, err
	}
	if release.Status == "pending_approval" {
		return nil, fmt.Errorf("release is pending approval")
	}
	return s.store.Open(ctx, release.SHA256)
}
func (s *Service) ApproveForWorkspace(ctx context.Context, workspaceID, name, version string) (entity.Release, error) {
	if s.workspaces == nil {
		return entity.Release{}, fmt.Errorf("workspace authorization is unavailable")
	}
	if err := s.workspaces.Authorize(ctx, workspaceID, entity.RoleAdmin); err != nil {
		return entity.Release{}, err
	}
	repository, ok := s.packages.(repo.WorkspaceArtifactRepository)
	if !ok {
		return entity.Release{}, fmt.Errorf("workspace release approval is unavailable")
	}
	release, err := repository.ApproveReleaseForWorkspace(ctx, workspaceID, name, version)
	if err != nil {
		return entity.Release{}, err
	}
	if s.audit != nil {
		_ = s.audit.AppendForWorkspace(ctx, workspaceID, auditEvent(ctx, entity.AuditEvent{Action: "release.approve", ResourceType: "release", ResourceID: name + "@" + version, At: time.Now().UTC()}))
	}
	if err := s.enqueue(ctx, workspaceID, "release.approved", map[string]any{"workspace_id": workspaceID, "name": name, "version": version, "sha256": release.SHA256}); err != nil {
		return release, fmt.Errorf("release approval committed but outbox enqueue failed: %w", err)
	}
	return release, nil
}

func (s *Service) enqueue(ctx context.Context, workspaceID, topic string, payload map[string]any) error {
	if s.outbox == nil {
		return nil
	}
	return s.outbox.Enqueue(ctx, entity.OutboxEvent{ID: uuid.NewString(), Topic: topic, Payload: payload, AvailableAt: time.Now().UTC()})
}
func (s *Service) List(ctx context.Context) ([]entity.Release, error) { return s.packages.List(ctx) }
func (s *Service) ListForWorkspace(ctx context.Context, workspaceID string) ([]entity.Release, error) {
	if p, ok := s.packages.(repo.WorkspacePackageRepository); ok {
		return p.ListForWorkspace(ctx, workspaceID)
	}
	return s.List(ctx)
}
func (s *Service) LookupIdempotency(ctx context.Context, workspaceID, key string) (entity.Release, bool, error) {
	if r, ok := s.packages.(repo.IdempotencyRepository); ok {
		return r.LookupIdempotency(ctx, workspaceID, key)
	}
	return entity.Release{}, false, nil
}
func (s *Service) StoreIdempotency(ctx context.Context, workspaceID, key string, v entity.Release) error {
	if r, ok := s.packages.(repo.IdempotencyRepository); ok {
		return r.StoreIdempotency(ctx, workspaceID, key, v)
	}
	return nil
}
