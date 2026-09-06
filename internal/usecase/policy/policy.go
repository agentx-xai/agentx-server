package policy

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
	repo   repo.PolicyRepository
	ws     *workspace.Service
	audit  repo.WorkspaceAuditWriter
	outbox repo.OutboxRepository
}

func New(r repo.PolicyRepository, ws *workspace.Service) *Service { return &Service{repo: r, ws: ws} }
func (s *Service) SetAudit(w repo.WorkspaceAuditWriter)           { s.audit = w }
func (s *Service) SetOutbox(w repo.OutboxRepository)              { s.outbox = w }
func auditEvent(ctx context.Context, event entity.AuditEvent) entity.AuditEvent {
	if principal, ok := auth.FromContext(ctx); ok {
		event.ActorID = principal.UserID
	}
	event.RequestID = auth.RequestID(ctx)
	return event
}

func (s *Service) Current(ctx context.Context, workspaceID string) (entity.Policy, error) {
	if err := s.ws.Authorize(ctx, workspaceID, entity.RoleViewer); err != nil {
		return entity.Policy{}, err
	}
	return s.repo.CurrentPolicy(ctx, workspaceID)
}

func (s *Service) Replace(ctx context.Context, workspaceID string, document map[string]any) (entity.Policy, error) {
	if err := s.ws.Authorize(ctx, workspaceID, entity.RoleAdmin); err != nil {
		return entity.Policy{}, err
	}
	if document == nil {
		return entity.Policy{}, errors.New("policy document is required")
	}
	current, err := s.repo.CurrentPolicy(ctx, workspaceID)
	if err != nil {
		return entity.Policy{}, err
	}
	policy := entity.Policy{ID: uuid.NewString(), WorkspaceID: workspaceID, Revision: current.Revision + 1, Document: document, CreatedAt: time.Now().UTC()}
	if err := s.repo.SavePolicy(ctx, policy); err != nil {
		return entity.Policy{}, err
	}
	if s.audit != nil {
		_ = s.audit.AppendForWorkspace(ctx, workspaceID, auditEvent(ctx, entity.AuditEvent{Action: "policy.update", ResourceType: "policy", ResourceID: policy.ID, At: policy.CreatedAt}))
	}
	if s.outbox != nil {
		_ = s.outbox.Enqueue(ctx, entity.OutboxEvent{ID: uuid.NewString(), Topic: "policy.updated", Payload: map[string]any{"workspace_id": workspaceID, "revision": policy.Revision}, AvailableAt: time.Now().UTC()})
	}
	return policy, nil
}
