package manifest

import (
	"agentx/server/internal/auth"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"agentx/server/internal/usecase/workspace"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repo   repo.ManifestRepository
	ws     *workspace.Service
	audit  repo.WorkspaceAuditWriter
	outbox repo.OutboxRepository
}

func New(r repo.ManifestRepository, ws *workspace.Service) *Service { return &Service{repo: r, ws: ws} }
func (s *Service) SetAudit(w repo.WorkspaceAuditWriter)             { s.audit = w }
func (s *Service) SetOutbox(w repo.OutboxRepository)                { s.outbox = w }
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
		return entity.TeamManifest{}, errors.New("manifest document is required")
	}
	current, err := s.repo.CurrentManifest(ctx, workspaceID)
	if err != nil {
		return entity.TeamManifest{}, err
	}
	manifest := entity.TeamManifest{ID: uuid.NewString(), WorkspaceID: workspaceID, Revision: current.Revision + 1, Document: document, CreatedAt: time.Now().UTC()}
	if err := s.repo.SaveManifest(ctx, manifest); err != nil {
		return entity.TeamManifest{}, err
	}
	if s.audit != nil {
		_ = s.audit.AppendForWorkspace(ctx, workspaceID, auditEvent(ctx, entity.AuditEvent{Action: "manifest.update", ResourceType: "manifest", ResourceID: manifest.ID, At: manifest.CreatedAt}))
	}
	if s.outbox != nil {
		_ = s.outbox.Enqueue(ctx, entity.OutboxEvent{ID: uuid.NewString(), Topic: "manifest.updated", Payload: map[string]any{"workspace_id": workspaceID, "revision": manifest.Revision}, AvailableAt: time.Now().UTC()})
	}
	return manifest, nil
}
