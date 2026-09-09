package workspace

import (
	"agentx/server/internal/apperror"
	"agentx/server/internal/auth"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"errors"
	"github.com/google/uuid"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

type Service struct {
	repo      repo.WorkspaceRepository
	audit     repo.WorkspaceAuditWriter
	outbox    repo.OutboxRepository
	uow       repo.UnitOfWork
	invites   repo.WorkspaceInvitationRepository
	artifacts repo.ArtifactLifecycleRepository
}

func New(r repo.WorkspaceRepository) *Service {
	service := &Service{repo: r}
	service.invites, _ = r.(repo.WorkspaceInvitationRepository)
	return service
}
func (s *Service) SetAudit(w repo.WorkspaceAuditWriter) { s.audit = w }
func (s *Service) SetOutbox(w repo.OutboxRepository)    { s.outbox = w }
func (s *Service) SetUnitOfWork(uow repo.UnitOfWork)    { s.uow = uow }
func (s *Service) SetArtifactLifecycle(lifecycle repo.ArtifactLifecycleRepository) {
	s.artifacts = lifecycle
}
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
func (s *Service) ListPage(ctx context.Context, request repo.PageRequest) (repo.Page[entity.Workspace], error) {
	if pager, ok := s.repo.(repo.WorkspacePageRepository); ok {
		return pager.ListPage(ctx, principalID(ctx), request)
	}
	items, err := s.List(ctx)
	if err != nil {
		return repo.Page[entity.Workspace]{}, err
	}
	return repo.PageSlice(items, request), nil
}
func (s *Service) Create(ctx context.Context, name, slug string) (entity.Workspace, error) {
	name, slug = strings.TrimSpace(name), strings.TrimSpace(strings.ToLower(slug))
	if name == "" || !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`).MatchString(slug) {
		return entity.Workspace{}, apperror.New(apperror.KindValidation, "name and valid slug are required")
	}
	now := time.Now().UTC()
	w := entity.Workspace{ID: uuid.NewString(), Name: name, Slug: slug, CreatedAt: now}
	membership := entity.Membership{WorkspaceID: w.ID, UserID: principalID(ctx), Role: entity.RoleOwner, CreatedAt: now}
	audit := auditEvent(ctx, entity.AuditEvent{Action: "workspace.create", ResourceType: "workspace", ResourceID: w.ID, At: now})
	outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "workspace.created", Payload: map[string]any{"workspace_id": w.ID, "name": w.Name, "slug": w.Slug}, AvailableAt: now}
	err := s.mutate(ctx, w.ID, func(txCtx context.Context) error { return s.repo.Create(txCtx, w, membership) }, &audit, &outbox)
	if errors.Is(err, repo.ErrConflict) {
		err = apperror.Wrap(apperror.KindConflict, "workspace slug already exists", err)
	}
	return w, err
}
func (s *Service) Authorize(ctx context.Context, workspaceID string, required entity.Role) error {
	m, err := s.repo.Membership(ctx, workspaceID, principalID(ctx))
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return apperror.Wrap(apperror.KindForbidden, "workspace access denied", err)
		}
		return err
	}
	if !m.Role.Allows(required) {
		return apperror.New(apperror.KindForbidden, "insufficient workspace role")
	}
	return nil
}
func (s *Service) Members(ctx context.Context, workspaceID string) ([]entity.Membership, error) {
	if err := s.Authorize(ctx, workspaceID, entity.RoleViewer); err != nil {
		return nil, err
	}
	return s.repo.Members(ctx, workspaceID)
}
func (s *Service) MembersPage(ctx context.Context, workspaceID string, request repo.PageRequest) (repo.Page[entity.Membership], error) {
	if err := s.Authorize(ctx, workspaceID, entity.RoleViewer); err != nil {
		return repo.Page[entity.Membership]{}, err
	}
	if pager, ok := s.repo.(repo.WorkspacePageRepository); ok {
		return pager.MembersPage(ctx, workspaceID, request)
	}
	items, err := s.repo.Members(ctx, workspaceID)
	if err != nil {
		return repo.Page[entity.Membership]{}, err
	}
	return repo.PageSlice(items, request), nil
}
func (s *Service) AddMember(ctx context.Context, workspaceID, userID string, role entity.Role) error {
	if err := s.Authorize(ctx, workspaceID, entity.RoleAdmin); err != nil {
		return err
	}
	if userID == "" || !memberRole(role) {
		return apperror.New(apperror.KindValidation, "valid member and role are required")
	}
	now := time.Now().UTC()
	membership := entity.Membership{WorkspaceID: workspaceID, UserID: userID, Role: role, CreatedAt: now}
	audit := auditEvent(ctx, entity.AuditEvent{Action: "member.add", ResourceType: "membership", ResourceID: userID, At: now})
	outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "membership.updated", Payload: map[string]any{"workspace_id": workspaceID, "user_id": userID, "role": role}, AvailableAt: now}
	err := s.mutate(ctx, workspaceID, func(txCtx context.Context) error { return s.repo.AddMember(txCtx, membership) }, &audit, &outbox)
	if errors.Is(err, repo.ErrConflict) {
		return apperror.Wrap(apperror.KindConflict, "membership already exists", err)
	}
	return err
}

const (
	defaultInvitationLifetime = 7 * 24 * time.Hour
	minInvitationLifetime     = 15 * time.Minute
	maxInvitationLifetime     = 30 * 24 * time.Hour
)

func canonicalEmail(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 254 || strings.Count(value, "@") != 1 {
		return "", apperror.New(apperror.KindValidation, "valid email is required")
	}
	address, err := mail.ParseAddress(value)
	if err != nil || address.Name != "" || !strings.EqualFold(address.Address, value) {
		return "", apperror.New(apperror.KindValidation, "valid email is required")
	}
	return strings.ToLower(address.Address), nil
}

func (s *Service) CreateInvitation(ctx context.Context, workspaceID, email string, role entity.Role, expiresIn time.Duration) (entity.WorkspaceInvitation, error) {
	if err := s.Authorize(ctx, workspaceID, entity.RoleAdmin); err != nil {
		return entity.WorkspaceInvitation{}, err
	}
	if s.invites == nil {
		return entity.WorkspaceInvitation{}, errors.New("invitation repository is unavailable")
	}
	canonical, err := canonicalEmail(email)
	if err != nil {
		return entity.WorkspaceInvitation{}, err
	}
	if !memberRole(role) {
		return entity.WorkspaceInvitation{}, apperror.New(apperror.KindValidation, "valid invitation role is required")
	}
	if expiresIn == 0 {
		expiresIn = defaultInvitationLifetime
	}
	if expiresIn < minInvitationLifetime || expiresIn > maxInvitationLifetime {
		return entity.WorkspaceInvitation{}, apperror.New(apperror.KindValidation, "invitation lifetime must be between 15 minutes and 30 days")
	}
	now := time.Now().UTC()
	invitation := entity.WorkspaceInvitation{ID: uuid.NewString(), WorkspaceID: workspaceID, Email: canonical, Role: role, CreatedBy: principalID(ctx), CreatedAt: now, ExpiresAt: now.Add(expiresIn)}
	invitation.SetStatus(now)
	audit := auditEvent(ctx, entity.AuditEvent{Action: "invitation.create", ResourceType: "workspace_invitation", ResourceID: invitation.ID, Metadata: map[string]any{"role": role, "expires_at": invitation.ExpiresAt}, At: now})
	outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "invitation.created", Payload: map[string]any{"workspace_id": workspaceID, "invitation_id": invitation.ID, "role": role, "expires_at": invitation.ExpiresAt}, AvailableAt: now}
	err = s.mutate(ctx, workspaceID, func(txCtx context.Context) error { return s.invites.CreateInvitation(txCtx, invitation) }, &audit, &outbox)
	if errors.Is(err, repo.ErrConflict) {
		return entity.WorkspaceInvitation{}, apperror.Wrap(apperror.KindConflict, "pending invitation already exists", err)
	}
	return invitation, err
}

func (s *Service) Invitations(ctx context.Context, workspaceID string, request repo.PageRequest) (repo.Page[entity.WorkspaceInvitation], error) {
	if err := s.Authorize(ctx, workspaceID, entity.RoleAdmin); err != nil {
		return repo.Page[entity.WorkspaceInvitation]{}, err
	}
	if s.invites == nil {
		return repo.Page[entity.WorkspaceInvitation]{}, errors.New("invitation repository is unavailable")
	}
	return s.invites.InvitationsForWorkspacePage(ctx, workspaceID, request)
}

func (s *Service) MyInvitations(ctx context.Context, request repo.PageRequest) (repo.Page[entity.WorkspaceInvitation], error) {
	if s.invites == nil {
		return repo.Page[entity.WorkspaceInvitation]{}, errors.New("invitation repository is unavailable")
	}
	principal, ok := auth.FromContext(ctx)
	if !ok || !principal.EmailVerified {
		return repo.Page[entity.WorkspaceInvitation]{}, apperror.New(apperror.KindForbidden, "a verified email claim is required")
	}
	email, err := canonicalEmail(principal.Email)
	if err != nil {
		return repo.Page[entity.WorkspaceInvitation]{}, apperror.New(apperror.KindForbidden, "a verified email claim is required")
	}
	return s.invites.InvitationsForEmailPage(ctx, email, request)
}

func (s *Service) RevokeInvitation(ctx context.Context, workspaceID, invitationID string) (entity.WorkspaceInvitation, error) {
	if err := s.Authorize(ctx, workspaceID, entity.RoleAdmin); err != nil {
		return entity.WorkspaceInvitation{}, err
	}
	if s.invites == nil || strings.TrimSpace(invitationID) == "" {
		return entity.WorkspaceInvitation{}, apperror.New(apperror.KindValidation, "invitation is required")
	}
	now := time.Now().UTC()
	var revoked entity.WorkspaceInvitation
	audit := auditEvent(ctx, entity.AuditEvent{Action: "invitation.revoke", ResourceType: "workspace_invitation", ResourceID: invitationID, At: now})
	outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "invitation.revoked", Payload: map[string]any{"workspace_id": workspaceID, "invitation_id": invitationID}, AvailableAt: now}
	err := s.mutate(ctx, workspaceID, func(txCtx context.Context) error {
		var revokeErr error
		revoked, revokeErr = s.invites.RevokeInvitation(txCtx, workspaceID, invitationID, principalID(ctx), now)
		return revokeErr
	}, &audit, &outbox)
	if errors.Is(err, repo.ErrNotFound) {
		return entity.WorkspaceInvitation{}, apperror.Wrap(apperror.KindNotFound, "invitation not found", err)
	}
	if errors.Is(err, repo.ErrConflict) {
		return entity.WorkspaceInvitation{}, apperror.Wrap(apperror.KindConflict, "invitation is not pending", err)
	}
	return revoked, err
}

func (s *Service) ClaimInvitation(ctx context.Context, invitationID string) (entity.WorkspaceInvitation, error) {
	if s.invites == nil || strings.TrimSpace(invitationID) == "" {
		return entity.WorkspaceInvitation{}, apperror.New(apperror.KindValidation, "invitation is required")
	}
	principal, ok := auth.FromContext(ctx)
	if !ok || principal.UserID == "" || principal.Issuer == "" || principal.Subject == "" || !principal.EmailVerified {
		return entity.WorkspaceInvitation{}, apperror.New(apperror.KindForbidden, "a verified email claim is required")
	}
	email, err := canonicalEmail(principal.Email)
	if err != nil {
		return entity.WorkspaceInvitation{}, apperror.New(apperror.KindForbidden, "a verified email claim is required")
	}
	invitation, err := s.invites.InvitationForEmail(ctx, invitationID, email)
	if errors.Is(err, repo.ErrNotFound) {
		return entity.WorkspaceInvitation{}, apperror.Wrap(apperror.KindNotFound, "invitation not found", err)
	}
	if err != nil {
		return entity.WorkspaceInvitation{}, err
	}
	if invitation.Status != "pending" {
		return entity.WorkspaceInvitation{}, apperror.New(apperror.KindConflict, "invitation is not pending")
	}
	now := time.Now().UTC()
	user := entity.User{ID: principal.UserID, Issuer: principal.Issuer, Subject: principal.Subject, Email: email, EmailVerified: true, CreatedAt: now}
	var claimed entity.WorkspaceInvitation
	audit := auditEvent(ctx, entity.AuditEvent{Action: "invitation.claim", ResourceType: "workspace_invitation", ResourceID: invitation.ID, Metadata: map[string]any{"role": invitation.Role}, At: now})
	outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "invitation.claimed", Payload: map[string]any{"workspace_id": invitation.WorkspaceID, "invitation_id": invitation.ID, "user_id": principal.UserID, "role": invitation.Role}, AvailableAt: now}
	err = s.mutate(ctx, invitation.WorkspaceID, func(txCtx context.Context) error {
		var claimErr error
		claimed, claimErr = s.invites.ClaimInvitation(txCtx, invitation, user, now)
		return claimErr
	}, &audit, &outbox)
	if errors.Is(err, repo.ErrConflict) {
		return entity.WorkspaceInvitation{}, apperror.Wrap(apperror.KindConflict, "invitation cannot be claimed", err)
	}
	return claimed, err
}
func (s *Service) UpdateMemberRole(ctx context.Context, workspaceID, userID string, role entity.Role) error {
	if err := s.Authorize(ctx, workspaceID, entity.RoleAdmin); err != nil {
		return err
	}
	if userID == "" || !memberRole(role) {
		return apperror.New(apperror.KindValidation, "valid member and role are required")
	}
	current, err := s.repo.Membership(ctx, workspaceID, userID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return apperror.Wrap(apperror.KindNotFound, "member not found", err)
		}
		return err
	}
	if current.Role == entity.RoleOwner {
		return apperror.New(apperror.KindConflict, "owner role cannot be changed")
	}
	now := time.Now().UTC()
	audit := auditEvent(ctx, entity.AuditEvent{Action: "member.role.update", ResourceType: "membership", ResourceID: userID, At: now})
	outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "membership.updated", Payload: map[string]any{"workspace_id": workspaceID, "user_id": userID, "role": role}, AvailableAt: now}
	err = s.mutate(ctx, workspaceID, func(txCtx context.Context) error { return s.repo.UpdateMemberRole(txCtx, workspaceID, userID, role) }, &audit, &outbox)
	if errors.Is(err, repo.ErrNotFound) {
		return apperror.Wrap(apperror.KindNotFound, "member not found", err)
	}
	return err
}
func (s *Service) RemoveMember(ctx context.Context, workspaceID, userID string) error {
	if err := s.Authorize(ctx, workspaceID, entity.RoleAdmin); err != nil {
		return err
	}
	if userID == "" {
		return apperror.New(apperror.KindValidation, "member is required")
	}
	current, err := s.repo.Membership(ctx, workspaceID, userID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return apperror.Wrap(apperror.KindNotFound, "member not found", err)
		}
		return err
	}
	if current.Role == entity.RoleOwner {
		return apperror.New(apperror.KindConflict, "owner membership cannot be removed")
	}
	now := time.Now().UTC()
	audit := auditEvent(ctx, entity.AuditEvent{Action: "member.remove", ResourceType: "membership", ResourceID: userID, At: now})
	outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "membership.removed", Payload: map[string]any{"workspace_id": workspaceID, "user_id": userID}, AvailableAt: now}
	err = s.mutate(ctx, workspaceID, func(txCtx context.Context) error { return s.repo.RemoveMember(txCtx, workspaceID, userID) }, &audit, &outbox)
	if errors.Is(err, repo.ErrNotFound) {
		return apperror.Wrap(apperror.KindNotFound, "member not found", err)
	}
	return err
}

func (s *Service) Delete(ctx context.Context, workspaceID string) error {
	if err := s.Authorize(ctx, workspaceID, entity.RoleOwner); err != nil {
		return err
	}
	now := time.Now().UTC()
	operation := func(txCtx context.Context) error {
		digests := []string{}
		if s.artifacts != nil {
			var err error
			digests, err = s.artifacts.DigestsForWorkspace(txCtx, workspaceID)
			if err != nil {
				return err
			}
		}
		if s.audit != nil {
			audit := auditEvent(ctx, entity.AuditEvent{Action: "workspace.delete", ResourceType: "workspace", ResourceID: workspaceID, Metadata: map[string]any{"artifact_candidates": len(digests)}, At: now})
			if err := s.audit.AppendForWorkspace(txCtx, workspaceID, audit); err != nil {
				return err
			}
		}
		if err := s.repo.DeleteWorkspace(txCtx, workspaceID); err != nil {
			return err
		}
		if s.artifacts != nil {
			if err := s.artifacts.RemoveWorkspaceReleases(txCtx, workspaceID); err != nil {
				return err
			}
		}
		if s.outbox != nil {
			for _, digest := range digests {
				if err := s.outbox.Enqueue(txCtx, entity.OutboxEvent{ID: uuid.NewString(), Topic: "artifact.cleanup", Payload: map[string]any{"workspace_id": workspaceID, "digest": digest, "reason": "workspace_deleted"}, AvailableAt: now}); err != nil {
					return err
				}
			}
			return s.outbox.Enqueue(txCtx, entity.OutboxEvent{ID: uuid.NewString(), Topic: "workspace.deleted", Payload: map[string]any{"workspace_id": workspaceID, "artifact_candidates": len(digests)}, AvailableAt: now})
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
		return apperror.Wrap(apperror.KindNotFound, "workspace not found", err)
	}
	if errors.Is(err, repo.ErrConflict) && strings.Contains(err.Error(), "active legal hold") {
		return apperror.Wrap(apperror.KindConflict, "workspace deletion is blocked by an active legal hold", err)
	}
	return err
}

func (s *Service) mutate(ctx context.Context, workspaceID string, change func(context.Context) error, audit *entity.AuditEvent, outbox *entity.OutboxEvent) error {
	operation := func(txCtx context.Context) error {
		if err := change(txCtx); err != nil {
			return err
		}
		if audit != nil && s.audit != nil {
			if err := s.audit.AppendForWorkspace(txCtx, workspaceID, *audit); err != nil {
				return err
			}
		}
		if outbox != nil && s.outbox != nil {
			return s.outbox.Enqueue(txCtx, *outbox)
		}
		return nil
	}
	if s.uow != nil {
		return s.uow.WithinTransaction(ctx, operation)
	}
	return operation(ctx)
}
func memberRole(role entity.Role) bool {
	return role == entity.RoleAdmin || role == entity.RoleDeveloper || role == entity.RoleViewer
}
