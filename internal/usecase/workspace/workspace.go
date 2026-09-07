package workspace

import (
	"agentx/server/internal/auth"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"errors"
	"github.com/google/uuid"
	"regexp"
	"strings"
	"time"
)

type Service struct {
	repo   repo.WorkspaceRepository
	audit  repo.WorkspaceAuditWriter
	outbox repo.OutboxRepository
}

func New(r repo.WorkspaceRepository) *Service           { return &Service{repo: r} }
func (s *Service) SetAudit(w repo.WorkspaceAuditWriter) { s.audit = w }
func (s *Service) SetOutbox(w repo.OutboxRepository)    { s.outbox = w }
func auditEvent(ctx context.Context, event entity.AuditEvent) entity.AuditEvent {
	if principal, ok := auth.FromContext(ctx); ok {
		event.ActorID = principal.UserID
	}
	event.RequestID = auth.RequestID(ctx)
	return event
}
func principalID(ctx context.Context) string {
	if p, ok := auth.FromContext(ctx); ok && p.UserID != "" {
		return p.UserID
	}
	return "anonymous"
}
func (s *Service) List(ctx context.Context) ([]entity.Workspace, error) {
	return s.repo.List(ctx, principalID(ctx))
}
func (s *Service) Create(ctx context.Context, name, slug string) (entity.Workspace, error) {
	name, slug = strings.TrimSpace(name), strings.TrimSpace(strings.ToLower(slug))
	if name == "" || len(name) > 128 || !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`).MatchString(slug) {
		return entity.Workspace{}, errors.New("name and valid slug are required")
	}
	now := time.Now().UTC()
	w := entity.Workspace{ID: uuid.NewString(), Name: name, Slug: slug, CreatedAt: now}
	if err := s.repo.Create(ctx, w, entity.Membership{WorkspaceID: w.ID, UserID: principalID(ctx), Role: entity.RoleOwner, CreatedAt: now}); err != nil {
		return w, err
	}
	if s.audit != nil {
		_ = s.audit.AppendForWorkspace(ctx, w.ID, auditEvent(ctx, entity.AuditEvent{Action: "workspace.create", ResourceType: "workspace", ResourceID: w.ID, At: now}))
	}
	s.enqueue(ctx, w.ID, "workspace.created", map[string]any{"workspace_id": w.ID, "name": w.Name, "slug": w.Slug})
	return w, nil
}
func (s *Service) Authorize(ctx context.Context, workspaceID string, required entity.Role) error {
	m, err := s.repo.Membership(ctx, workspaceID, principalID(ctx))
	if err != nil {
		return errors.New("workspace access denied")
	}
	if !m.Role.Allows(required) {
		return errors.New("insufficient workspace role")
	}
	return nil
}
func (s *Service) Members(ctx context.Context, workspaceID string) ([]entity.Membership, error) {
	if err := s.Authorize(ctx, workspaceID, entity.RoleViewer); err != nil {
		return nil, err
	}
	return s.repo.Members(ctx, workspaceID)
}
func (s *Service) AddMember(ctx context.Context, workspaceID, userID string, role entity.Role) error {
	if err := s.Authorize(ctx, workspaceID, entity.RoleAdmin); err != nil {
		return err
	}
	if userID == "" || len(userID) > 255 || !memberRole(role) {
		return errors.New("valid member and role are required")
	}
	now := time.Now().UTC()
	if err := s.repo.AddMember(ctx, entity.Membership{WorkspaceID: workspaceID, UserID: userID, Role: role, CreatedAt: now}); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.AppendForWorkspace(ctx, workspaceID, auditEvent(ctx, entity.AuditEvent{Action: "member.add", ResourceType: "membership", ResourceID: userID, At: now}))
	}
	s.enqueue(ctx, workspaceID, "membership.updated", map[string]any{"workspace_id": workspaceID, "user_id": userID, "role": role})
	return nil
}
func (s *Service) UpdateMemberRole(ctx context.Context, workspaceID, userID string, role entity.Role) error {
	if err := s.Authorize(ctx, workspaceID, entity.RoleAdmin); err != nil {
		return err
	}
	if userID == "" || len(userID) > 255 || !memberRole(role) {
		return errors.New("valid member and role are required")
	}
	current, err := s.repo.Membership(ctx, workspaceID, userID)
	if err != nil {
		return err
	}
	if current.Role == entity.RoleOwner {
		return errors.New("owner role cannot be changed")
	}
	if err := s.repo.UpdateMemberRole(ctx, workspaceID, userID, role); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.AppendForWorkspace(ctx, workspaceID, auditEvent(ctx, entity.AuditEvent{Action: "member.role.update", ResourceType: "membership", ResourceID: userID, At: time.Now().UTC()}))
	}
	s.enqueue(ctx, workspaceID, "membership.updated", map[string]any{"workspace_id": workspaceID, "user_id": userID, "role": role})
	return nil
}
func (s *Service) RemoveMember(ctx context.Context, workspaceID, userID string) error {
	if err := s.Authorize(ctx, workspaceID, entity.RoleAdmin); err != nil {
		return err
	}
	if userID == "" || len(userID) > 255 {
		return errors.New("member is required")
	}
	current, err := s.repo.Membership(ctx, workspaceID, userID)
	if err != nil {
		return err
	}
	if current.Role == entity.RoleOwner {
		return errors.New("owner membership cannot be removed")
	}
	if err := s.repo.RemoveMember(ctx, workspaceID, userID); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.AppendForWorkspace(ctx, workspaceID, auditEvent(ctx, entity.AuditEvent{Action: "member.remove", ResourceType: "membership", ResourceID: userID, At: time.Now().UTC()}))
	}
	s.enqueue(ctx, workspaceID, "membership.removed", map[string]any{"workspace_id": workspaceID, "user_id": userID})
	return nil
}

func (s *Service) Delete(ctx context.Context, workspaceID string) error {
	if err := s.Authorize(ctx, workspaceID, entity.RoleOwner); err != nil {
		return err
	}
	if err := s.repo.DeleteWorkspace(ctx, workspaceID); err != nil {
		return err
	}
	s.enqueue(ctx, workspaceID, "workspace.deleted", map[string]any{"workspace_id": workspaceID})
	return nil
}

func (s *Service) enqueue(ctx context.Context, workspaceID, topic string, payload map[string]any) {
	if s.outbox == nil {
		return
	}
	_ = s.outbox.Enqueue(ctx, entity.OutboxEvent{ID: uuid.NewString(), Topic: topic, Payload: payload, AvailableAt: time.Now().UTC()})
}
func memberRole(role entity.Role) bool {
	return role == entity.RoleAdmin || role == entity.RoleDeveloper || role == entity.RoleViewer
}
