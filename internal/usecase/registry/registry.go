package registry

import (
	"agentx/server/internal/apperror"
	"agentx/server/internal/auth"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"agentx/server/internal/usecase/workspace"
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

type Service struct {
	store          repo.ArtifactStore
	packages       repo.PackageRepository
	verifier       repo.SignatureVerifier
	audit          repo.WorkspaceAuditWriter
	workspaces     *workspace.Service
	policy         repo.PolicyRepository
	outbox         repo.OutboxRepository
	uow            repo.UnitOfWork
	idempotencyMu  sync.Mutex
	requireArchive bool
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
func (s *Service) SetUnitOfWork(uow repo.UnitOfWork)    { s.uow = uow }
func (s *Service) SetPackageArchiveRequired(required bool) {
	s.requireArchive = required
}
func auditEvent(ctx context.Context, event entity.AuditEvent) entity.AuditEvent {
	if principal, ok := auth.FromContext(ctx); ok {
		event.ActorID = principal.UserID
	}
	event.RequestID = auth.RequestID(ctx)
	return event
}
func (s *Service) prepare(ctx context.Context, name, version string, r io.Reader, signatures ...string) (release entity.Release, err error) {
	ctx, span := otel.Tracer("agentx/server/registry").Start(ctx, "registry.prepare")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "release preparation failed")
		}
		span.End()
	}()
	prepared, payload, err := s.preparePayload(ctx, name, version, r, signatures...)
	if err != nil {
		return entity.Release{}, err
	}
	return s.storePrepared(ctx, prepared, payload)
}

func (s *Service) preparePayload(ctx context.Context, name, version string, reader io.Reader, signatures ...string) (entity.Release, []byte, error) {
	ctx, span := otel.Tracer("agentx/server/registry").Start(ctx, "registry.prepare")
	defer span.End()
	name, version = strings.TrimSpace(name), strings.TrimSpace(version)
	if name == "" || !regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$`).MatchString(version) {
		return entity.Release{}, nil, apperror.New(apperror.KindValidation, "package name and version are required")
	}
	payload, err := io.ReadAll(io.LimitReader(reader, maxPackageUploadSize+1))
	if err != nil {
		return entity.Release{}, nil, err
	}
	if int64(len(payload)) > maxPackageUploadSize {
		return entity.Release{}, nil, apperror.New(apperror.KindValidation, fmt.Sprintf("artifact exceeds %d bytes", maxPackageUploadSize))
	}
	if s.requireArchive {
		if err = validatePackageArchive(payload); err != nil {
			return entity.Release{}, nil, err
		}
	}
	if s.verifier != nil {
		if len(signatures) == 0 || signatures[0] == "" {
			return entity.Release{}, nil, apperror.New(apperror.KindValidation, "artifact signature is required")
		}
		if err = s.verifier.Verify(ctx, payload, signatures[0]); err != nil {
			return entity.Release{}, nil, apperror.Wrap(apperror.KindValidation, "artifact signature verification failed", err)
		}
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	release := entity.Release{Name: name, Version: version, SHA256: digest, Size: int64(len(payload)), Status: "published", CreatedAt: time.Now().UTC()}
	if s.verifier != nil {
		release.Signature, release.SignatureStatus = signatures[0], "verified"
	}
	return release, payload, nil
}

func (s *Service) storePrepared(ctx context.Context, prepared entity.Release, payload []byte) (entity.Release, error) {
	stored, err := s.store.Put(ctx, prepared.Name, bytes.NewReader(payload))
	if err != nil {
		return entity.Release{}, err
	}
	if stored.SHA256 != prepared.SHA256 || stored.Size != prepared.Size {
		return entity.Release{}, fmt.Errorf("artifact store returned inconsistent digest or size")
	}
	return prepared, nil
}

const (
	maxPackageUploadSize       int64 = 51 << 20
	maxPackageUncompressedSize int64 = 100 << 20
	maxPackageFiles                  = 4096
)

func validatePackageArchive(payload []byte) error {
	gz, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return apperror.Wrap(apperror.KindValidation, "artifact must be a gzip tar archive", err)
	}
	defer gz.Close()
	gz.Multistream(false)
	reader := tar.NewReader(gz)
	seen := map[string]bool{}
	hasSkill := false
	var total int64
	for count := 0; ; count++ {
		if count >= maxPackageFiles {
			return apperror.New(apperror.KindValidation, fmt.Sprintf("artifact contains more than %d entries", maxPackageFiles))
		}
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return apperror.Wrap(apperror.KindValidation, "invalid artifact archive", err)
		}
		name := strings.TrimPrefix(header.Name, "./")
		clean := path.Clean(name)
		if name == "" || clean == "." || clean != name || path.IsAbs(name) || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(name, "\\") {
			return apperror.New(apperror.KindValidation, fmt.Sprintf("unsafe artifact path %q", header.Name))
		}
		if seen[clean] {
			return apperror.New(apperror.KindValidation, fmt.Sprintf("duplicate artifact path %q", clean))
		}
		seen[clean] = true
		if forbiddenPackageName(clean) {
			return apperror.New(apperror.KindValidation, fmt.Sprintf("artifact contains forbidden credential path %q", clean))
		}
		switch header.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg, tar.TypeRegA:
		default:
			return apperror.New(apperror.KindValidation, fmt.Sprintf("artifact entry %q is not a regular file or directory", clean))
		}
		if header.Mode&0o111 != 0 {
			return apperror.New(apperror.KindValidation, fmt.Sprintf("artifact executable file %q is not allowed", clean))
		}
		if header.Size < 0 || header.Size > maxPackageUncompressedSize-total {
			return apperror.New(apperror.KindValidation, fmt.Sprintf("artifact uncompressed content exceeds %d bytes", maxPackageUncompressedSize))
		}
		total += header.Size
		if _, err := io.Copy(io.Discard, reader); err != nil {
			return apperror.Wrap(apperror.KindValidation, fmt.Sprintf("read artifact entry %q", clean), err)
		}
		if clean == "SKILL.md" {
			hasSkill = true
		}
	}
	if !hasSkill {
		return apperror.New(apperror.KindValidation, "artifact root must contain SKILL.md")
	}
	return nil
}

func forbiddenPackageName(name string) bool {
	base := strings.ToLower(path.Base(name))
	if base == ".env" || strings.HasPrefix(base, ".env.") || base == "credentials.json" || base == "cookies.json" || base == "session.json" || base == "id_rsa" || base == "id_ed25519" {
		return true
	}
	return strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key")
}
func (s *Service) Publish(ctx context.Context, name, version string, r io.Reader, signatures ...string) (entity.Release, error) {
	v, err := s.prepare(ctx, name, version, r, signatures...)
	if err != nil {
		return v, err
	}
	if err = s.packages.Save(ctx, v); err != nil {
		return v, releaseRepositoryError(err)
	}
	return v, nil
}
func (s *Service) PublishForWorkspace(ctx context.Context, workspaceID, name, version string, r io.Reader, signatures ...string) (entity.Release, error) {
	status, err := s.releaseStatus(ctx, workspaceID, signatures...)
	if err != nil {
		return entity.Release{}, err
	}
	p, ok := s.packages.(repo.WorkspacePackageRepository)
	if !ok {
		return entity.Release{}, fmt.Errorf("workspace package persistence is unavailable")
	}
	prepared, payload, err := s.preparePayload(ctx, name, version, r, signatures...)
	if err != nil {
		return entity.Release{}, err
	}
	prepared.Status = status
	var v entity.Release
	stored := false
	err = s.withArtifactReferenceLock(ctx, prepared.SHA256, func(lockCtx context.Context) error {
		var storeErr error
		v, storeErr = s.storePrepared(lockCtx, prepared, payload)
		if storeErr != nil {
			return storeErr
		}
		stored = true
		return p.SaveForWorkspace(lockCtx, workspaceID, v)
	})
	if err != nil {
		if stored {
			err = errors.Join(err, s.cleanupFailedPublish(ctx, workspaceID, prepared.SHA256))
		}
		return v, releaseRepositoryError(err)
	}
	if err = s.afterWorkspacePublish(ctx, workspaceID, v); err != nil {
		return v, err
	}
	return v, nil
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
		return "", apperror.New(apperror.KindValidation, "release signature is required by workspace policy")
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
	if strings.TrimSpace(key) == "" {
		return entity.Release{}, false, apperror.New(apperror.KindValidation, "idempotency key is required")
	}
	if len(key) > 255 {
		return entity.Release{}, false, apperror.New(apperror.KindValidation, "idempotency key must be at most 255 bytes")
	}
	status, err := s.releaseStatus(ctx, workspaceID, signatures...)
	if err != nil {
		return entity.Release{}, false, err
	}
	prepared, payload, err := s.preparePayload(ctx, name, version, r, signatures...)
	if err != nil {
		return entity.Release{}, false, err
	}
	prepared.Status = status
	record := entity.IdempotencyRecord{Fingerprint: idempotencyFingerprint(prepared), Release: prepared}
	if repository, ok := s.packages.(repo.AtomicIdempotencyRepository); ok {
		audit := auditEvent(ctx, entity.AuditEvent{Action: "release.publish", ResourceType: "release", ResourceID: prepared.Name + "@" + prepared.Version, At: prepared.CreatedAt})
		outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "release.published", Payload: map[string]any{"workspace_id": workspaceID, "name": prepared.Name, "version": prepared.Version, "sha256": prepared.SHA256, "status": prepared.Status}, AvailableAt: time.Now().UTC()}
		var saved entity.Release
		var replayed bool
		stored := false
		err := s.withArtifactReferenceLock(ctx, prepared.SHA256, func(lockCtx context.Context) error {
			var storeErr error
			if _, storeErr = s.storePrepared(lockCtx, prepared, payload); storeErr != nil {
				return storeErr
			}
			stored = true
			var saveErr error
			saved, replayed, saveErr = repository.SaveForWorkspaceIdempotent(lockCtx, workspaceID, key, record, audit, outbox)
			return saveErr
		})
		if err != nil && stored {
			err = errors.Join(err, s.cleanupFailedPublish(ctx, workspaceID, prepared.SHA256))
		}
		err = releaseRepositoryError(err)
		if err != nil || replayed {
			return saved, replayed, err
		}
		return saved, false, nil
	}
	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	if existing, found, err := s.LookupIdempotency(ctx, workspaceID, key); err != nil {
		return entity.Release{}, false, releaseRepositoryError(err)
	} else if found {
		if !idempotencyMatches(existing, record) {
			return entity.Release{}, false, apperror.New(apperror.KindConflict, "idempotency key already used for a different request")
		}
		return existing.Release, true, nil
	}
	packages, ok := s.packages.(repo.WorkspacePackageRepository)
	if !ok {
		return entity.Release{}, false, fmt.Errorf("workspace package persistence is unavailable")
	}
	var v entity.Release
	stored := false
	err = s.withArtifactReferenceLock(ctx, prepared.SHA256, func(lockCtx context.Context) error {
		var storeErr error
		v, storeErr = s.storePrepared(lockCtx, prepared, payload)
		if storeErr != nil {
			return storeErr
		}
		stored = true
		return packages.SaveForWorkspace(lockCtx, workspaceID, v)
	})
	if err != nil {
		if stored {
			err = errors.Join(err, s.cleanupFailedPublish(ctx, workspaceID, prepared.SHA256))
		}
		return v, false, releaseRepositoryError(err)
	}
	if err = s.StoreIdempotency(ctx, workspaceID, key, record); err != nil {
		return v, false, err
	}
	if err = s.afterWorkspacePublish(ctx, workspaceID, v); err != nil {
		return v, false, err
	}
	return v, false, nil
}

func (s *Service) withArtifactReferenceLock(ctx context.Context, digest string, operation func(context.Context) error) error {
	if lifecycle, ok := s.packages.(repo.ArtifactLifecycleRepository); ok {
		return lifecycle.WithArtifactReferenceLock(ctx, digest, operation)
	}
	return operation(ctx)
}

func (s *Service) cleanupFailedPublish(ctx context.Context, workspaceID, digest string) error {
	cleanupErr := s.withArtifactReferenceLock(ctx, digest, func(lockCtx context.Context) error {
		if lifecycle, ok := s.packages.(repo.ArtifactLifecycleRepository); ok {
			referenced, err := lifecycle.ArtifactReferenced(lockCtx, digest)
			if err != nil || referenced {
				return err
			}
		}
		return s.store.Delete(lockCtx, digest)
	})
	if cleanupErr == nil || s.outbox == nil {
		return cleanupErr
	}
	enqueueErr := s.outbox.Enqueue(ctx, entity.OutboxEvent{ID: uuid.NewString(), Topic: "artifact.cleanup", Payload: map[string]any{"workspace_id": workspaceID, "digest": digest, "reason": "failed_publish"}, AvailableAt: time.Now().UTC()})
	return errors.Join(cleanupErr, enqueueErr)
}

func idempotencyFingerprint(v entity.Release) string {
	raw := strings.Join([]string{v.Name, v.Version, v.SHA256, v.Signature}, "\x00")
	return fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
}

func idempotencyMatches(existing, requested entity.IdempotencyRecord) bool {
	if existing.Fingerprint != "" {
		return existing.Fingerprint == requested.Fingerprint
	}
	return idempotencyFingerprint(existing.Release) == requested.Fingerprint
}

func (s *Service) afterWorkspacePublish(ctx context.Context, workspaceID string, v entity.Release) error {
	if s.audit != nil {
		if err := s.audit.AppendForWorkspace(ctx, workspaceID, auditEvent(ctx, entity.AuditEvent{Action: "release.publish", ResourceType: "release", ResourceID: v.Name + "@" + v.Version, At: v.CreatedAt})); err != nil {
			return fmt.Errorf("release committed but audit append failed: %w", err)
		}
	}
	if err := s.enqueue(ctx, workspaceID, "release.published", map[string]any{"workspace_id": workspaceID, "name": v.Name, "version": v.Version, "sha256": v.SHA256, "status": v.Status}); err != nil {
		if s.audit != nil {
			if auditErr := s.audit.AppendForWorkspace(ctx, workspaceID, auditEvent(ctx, entity.AuditEvent{Action: "outbox.enqueue_failed", ResourceType: "release", ResourceID: v.Name + "@" + v.Version, Metadata: map[string]any{"error": err.Error()}, At: time.Now().UTC()})); auditErr != nil {
				return fmt.Errorf("release committed but outbox enqueue failed: %v; failure audit append failed: %w", err, auditErr)
			}
		}
		return fmt.Errorf("release committed but outbox enqueue failed: %w", err)
	}
	return nil
}
func (s *Service) Open(ctx context.Context, digest string) (io.ReadCloser, error) {
	return s.store.Open(ctx, digest)
}
func (s *Service) OpenForWorkspace(ctx context.Context, workspaceID, digest string) (io.ReadCloser, error) {
	if repository, ok := s.packages.(repo.WorkspaceArtifactRepository); ok {
		release, err := repository.FindReleaseByDigestForWorkspace(ctx, workspaceID, digest)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return nil, apperror.Wrap(apperror.KindNotFound, "artifact not found", err)
			}
			return nil, err
		}
		if release.Status == "pending_approval" {
			return nil, apperror.New(apperror.KindNotFound, "release not found")
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
		if errors.Is(err, repo.ErrNotFound) {
			return nil, apperror.Wrap(apperror.KindNotFound, "release not found", err)
		}
		return nil, err
	}
	if release.Status == "pending_approval" {
		return nil, apperror.New(apperror.KindNotFound, "release not found")
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
	var release entity.Release
	operation := func(txCtx context.Context) error {
		var err error
		release, err = repository.ApproveReleaseForWorkspace(txCtx, workspaceID, name, version)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if s.audit != nil {
			if err = s.audit.AppendForWorkspace(txCtx, workspaceID, auditEvent(ctx, entity.AuditEvent{Action: "release.approve", ResourceType: "release", ResourceID: name + "@" + version, At: now})); err != nil {
				return err
			}
		}
		if s.outbox != nil {
			return s.outbox.Enqueue(txCtx, entity.OutboxEvent{ID: uuid.NewString(), Topic: "release.approved", Payload: map[string]any{"workspace_id": workspaceID, "name": name, "version": version, "sha256": release.SHA256}, AvailableAt: now})
		}
		return nil
	}
	var err error
	if s.uow != nil {
		err = s.uow.WithinTransaction(ctx, operation)
	} else {
		err = operation(ctx)
	}
	if errors.Is(err, repo.ErrNotFound) {
		err = apperror.Wrap(apperror.KindNotFound, "release not found", err)
	} else if errors.Is(err, repo.ErrConflict) {
		err = apperror.Wrap(apperror.KindConflict, "release is not pending approval", err)
	}
	return release, err
}

func releaseRepositoryError(err error) error {
	switch {
	case errors.Is(err, repo.ErrIdempotencyConflict):
		return apperror.Wrap(apperror.KindConflict, "idempotency key already used for a different request", err)
	case errors.Is(err, repo.ErrConflict):
		return apperror.Wrap(apperror.KindConflict, "release version already exists", err)
	default:
		return err
	}
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
	return nil, fmt.Errorf("workspace package persistence is unavailable")
}

func (s *Service) ListPage(ctx context.Context, request repo.PageRequest) (repo.Page[entity.Release], error) {
	if pager, ok := s.packages.(repo.PackagePageRepository); ok {
		return pager.ListPage(ctx, request)
	}
	items, err := s.List(ctx)
	if err != nil {
		return repo.Page[entity.Release]{}, err
	}
	return repo.PageSlice(items, request), nil
}

func (s *Service) ListForWorkspacePage(ctx context.Context, workspaceID string, request repo.PageRequest) (repo.Page[entity.Release], error) {
	if pager, ok := s.packages.(repo.WorkspacePackagePageRepository); ok {
		return pager.ListPageForWorkspace(ctx, workspaceID, request)
	}
	items, err := s.ListForWorkspace(ctx, workspaceID)
	if err != nil {
		return repo.Page[entity.Release]{}, err
	}
	return repo.PageSlice(items, request), nil
}
func (s *Service) LookupIdempotency(ctx context.Context, workspaceID, key string) (entity.IdempotencyRecord, bool, error) {
	if r, ok := s.packages.(repo.IdempotencyRepository); ok {
		return r.LookupIdempotency(ctx, workspaceID, key)
	}
	return entity.IdempotencyRecord{}, false, nil
}
func (s *Service) StoreIdempotency(ctx context.Context, workspaceID, key string, v entity.IdempotencyRecord) error {
	if r, ok := s.packages.(repo.IdempotencyRepository); ok {
		return r.StoreIdempotency(ctx, workspaceID, key, v)
	}
	return nil
}
