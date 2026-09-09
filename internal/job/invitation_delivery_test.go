package job

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"errors"
	"testing"
	"time"
)

type invitationRepositoryStub struct {
	invitation entity.WorkspaceInvitation
	err        error
}

func (s invitationRepositoryStub) CreateInvitation(context.Context, entity.WorkspaceInvitation) error {
	return nil
}
func (s invitationRepositoryStub) Invitation(context.Context, string, string) (entity.WorkspaceInvitation, error) {
	return s.invitation, s.err
}
func (s invitationRepositoryStub) InvitationForEmail(context.Context, string, string) (entity.WorkspaceInvitation, error) {
	return entity.WorkspaceInvitation{}, nil
}
func (s invitationRepositoryStub) InvitationsForWorkspacePage(context.Context, string, repo.PageRequest) (repo.Page[entity.WorkspaceInvitation], error) {
	return repo.Page[entity.WorkspaceInvitation]{}, nil
}
func (s invitationRepositoryStub) InvitationsForEmailPage(context.Context, string, repo.PageRequest) (repo.Page[entity.WorkspaceInvitation], error) {
	return repo.Page[entity.WorkspaceInvitation]{}, nil
}
func (s invitationRepositoryStub) RevokeInvitation(context.Context, string, string, string, time.Time) (entity.WorkspaceInvitation, error) {
	return entity.WorkspaceInvitation{}, nil
}
func (s invitationRepositoryStub) ClaimInvitation(context.Context, entity.WorkspaceInvitation, entity.User, time.Time) (entity.WorkspaceInvitation, error) {
	return entity.WorkspaceInvitation{}, nil
}

type invitationSenderStub struct {
	called int
	err    error
}

func (s *invitationSenderStub) SendInvitation(context.Context, entity.WorkspaceInvitation) error {
	s.called++
	return s.err
}

func TestInvitationDeliverySendsOnlyPendingInvitations(t *testing.T) {
	sender := &invitationSenderStub{}
	pending := entity.WorkspaceInvitation{ID: "invite", WorkspaceID: "workspace", Status: "pending"}
	handler := InvitationDeliveryHandler{Invitations: invitationRepositoryStub{invitation: pending}, Sender: sender}
	if err := handler.Handle(context.Background(), map[string]any{"workspace_id": "workspace", "invitation_id": "invite"}); err != nil {
		t.Fatal(err)
	}
	if sender.called != 1 {
		t.Fatalf("sender called %d times", sender.called)
	}

	sender.called = 0
	handler.Invitations = invitationRepositoryStub{invitation: entity.WorkspaceInvitation{Status: "revoked"}}
	if err := handler.Handle(context.Background(), map[string]any{"workspace_id": "workspace", "invitation_id": "invite"}); err != nil {
		t.Fatal(err)
	}
	if sender.called != 0 {
		t.Fatalf("sender called for revoked invitation")
	}
}

func TestInvitationDeliveryReturnsFailuresForWorkerRetry(t *testing.T) {
	sender := &invitationSenderStub{err: errors.New("smtp unavailable")}
	handler := InvitationDeliveryHandler{Invitations: invitationRepositoryStub{invitation: entity.WorkspaceInvitation{Status: "pending"}}, Sender: sender}
	if err := handler.Handle(context.Background(), map[string]any{"workspace_id": "workspace", "invitation_id": "invite"}); err == nil {
		t.Fatal("expected delivery error")
	}
	if err := handler.Handle(context.Background(), map[string]any{"invitation_id": "invite"}); err == nil {
		t.Fatal("expected payload validation error")
	}
}
