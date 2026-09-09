package policy

import (
	"agentx/server/internal/apperror"
	"agentx/server/internal/auth"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"agentx/server/internal/usecase/workspace"
	"context"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repo                           repo.PolicyRepository
	ws                             *workspace.Service
	audit                          repo.WorkspaceAuditWriter
	outbox                         repo.OutboxRepository
	uow                            repo.UnitOfWork
	signatureVerificationAvailable bool
}

func New(r repo.PolicyRepository, ws *workspace.Service) *Service { return &Service{repo: r, ws: ws} }
func (s *Service) SetAudit(w repo.WorkspaceAuditWriter)           { s.audit = w }
func (s *Service) SetOutbox(w repo.OutboxRepository)              { s.outbox = w }
func (s *Service) SetUnitOfWork(uow repo.UnitOfWork)              { s.uow = uow }
func (s *Service) SetSignatureVerificationAvailable(available bool) {
	s.signatureVerificationAvailable = available
}
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
		return entity.Policy{}, apperror.New(apperror.KindValidation, "policy document is required")
	}
	for key, value := range document {
		if key != "require_signature" && key != "require_approval" {
			return entity.Policy{}, apperror.New(apperror.KindValidation, "policy contains an unsupported setting")
		}
		if _, ok := value.(bool); !ok {
			return entity.Policy{}, apperror.New(apperror.KindValidation, "policy settings must be boolean")
		}
	}
	if required, _ := document["require_signature"].(bool); required && !s.signatureVerificationAvailable {
		return entity.Policy{}, apperror.New(apperror.KindValidation, "signature verification is not configured")
	}
	current, err := s.repo.CurrentPolicy(ctx, workspaceID)
	if err != nil {
		return entity.Policy{}, err
	}
	policy := entity.Policy{ID: uuid.NewString(), WorkspaceID: workspaceID, Revision: current.Revision + 1, Document: document, CreatedAt: time.Now().UTC()}
	audit := auditEvent(ctx, entity.AuditEvent{Action: "policy.update", ResourceType: "policy", ResourceID: policy.ID, At: policy.CreatedAt})
	outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "policy.updated", Payload: map[string]any{"workspace_id": workspaceID, "revision": policy.Revision}, AvailableAt: policy.CreatedAt}
	operation := func(txCtx context.Context) error {
		if err := s.repo.SavePolicy(txCtx, policy); err != nil {
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
		return policy, s.uow.WithinTransaction(ctx, operation)
	}
	return policy, operation(ctx)
}
