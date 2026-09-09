package manifest

import (
	"agentx/server/internal/apperror"
	"agentx/server/internal/auth"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"agentx/server/internal/usecase/workspace"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repo     repo.ManifestRepository
	ws       *workspace.Service
	audit    repo.WorkspaceAuditWriter
	outbox   repo.OutboxRepository
	uow      repo.UnitOfWork
	packages repo.PackageRepository
}

func New(r repo.ManifestRepository, ws *workspace.Service) *Service { return &Service{repo: r, ws: ws} }
func (s *Service) SetAudit(w repo.WorkspaceAuditWriter)             { s.audit = w }
func (s *Service) SetOutbox(w repo.OutboxRepository)                { s.outbox = w }
func (s *Service) SetUnitOfWork(uow repo.UnitOfWork)                { s.uow = uow }
func (s *Service) SetPackages(packages repo.PackageRepository)      { s.packages = packages }
func auditEvent(ctx context.Context, event entity.AuditEvent) entity.AuditEvent {
	if principal, ok := auth.FromContext(ctx); ok {
		event.ActorID = principal.UserID
	}
	event.RequestID = auth.RequestID(ctx)
	return event
}

func (s *Service) Current(ctx context.Context, workspaceID string) (entity.TeamManifest, error) {
	if err := s.ws.Authorize(ctx, workspaceID, entity.RoleViewer); err != nil {
		return entity.TeamManifest{}, err
	}
	return s.repo.CurrentManifest(ctx, workspaceID)
}

func (s *Service) Replace(ctx context.Context, workspaceID string, document map[string]any) (entity.TeamManifest, error) {
	if err := s.ws.Authorize(ctx, workspaceID, entity.RoleAdmin); err != nil {
		return entity.TeamManifest{}, err
	}
	if document == nil {
		return entity.TeamManifest{}, apperror.New(apperror.KindValidation, "manifest document is required")
	}
	desired, err := entity.ParseDesiredManifest(document)
	if err != nil {
		return entity.TeamManifest{}, apperror.Wrap(apperror.KindValidation, "invalid manifest document", err)
	}
	if s.packages != nil {
		packages, ok := s.packages.(repo.WorkspaceArtifactRepository)
		if !ok {
			return entity.TeamManifest{}, fmt.Errorf("workspace package lookup is unavailable")
		}
		for _, item := range desired {
			release, findErr := packages.FindReleaseForWorkspace(ctx, workspaceID, item.Name, item.Version)
			if errors.Is(findErr, repo.ErrNotFound) {
				return entity.TeamManifest{}, apperror.New(apperror.KindValidation, "manifest references a release that does not exist")
			}
			if findErr != nil {
				return entity.TeamManifest{}, findErr
			}
			if release.SHA256 != item.SHA256 {
				return entity.TeamManifest{}, apperror.New(apperror.KindValidation, "manifest release digest does not match")
			}
			if release.Status == "pending_approval" {
				return entity.TeamManifest{}, apperror.New(apperror.KindValidation, "manifest release is not downloadable")
			}
		}
	}
	current, err := s.repo.CurrentManifest(ctx, workspaceID)
	if err != nil {
		return entity.TeamManifest{}, err
	}
	manifest := entity.TeamManifest{ID: uuid.NewString(), WorkspaceID: workspaceID, Revision: current.Revision + 1, Document: document, CreatedAt: time.Now().UTC()}
	audit := auditEvent(ctx, entity.AuditEvent{Action: "manifest.update", ResourceType: "manifest", ResourceID: manifest.ID, At: manifest.CreatedAt})
	outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "manifest.updated", Payload: map[string]any{"workspace_id": workspaceID, "revision": manifest.Revision}, AvailableAt: manifest.CreatedAt}
	operation := func(txCtx context.Context) error {
		if err := s.repo.SaveManifest(txCtx, manifest); err != nil {
			return err
		}
		if s.audit != nil {
			if err := s.audit.AppendForWorkspace(txCtx, workspaceID, audit); err != nil {
				return err
			}
		}
		if s.outbox != nil {
			return s.outbox.Enqueue(txCtx, outbox)
		}
		return nil
	}
	if s.uow != nil {
		return manifest, s.uow.WithinTransaction(ctx, operation)
	}
	return manifest, operation(ctx)
}
