package job

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"fmt"
)

type InvitationSender interface {
	SendInvitation(context.Context, entity.WorkspaceInvitation) error
}

type InvitationDeliveryHandler struct {
	Invitations repo.WorkspaceInvitationRepository
	Sender      InvitationSender
}

func (h InvitationDeliveryHandler) Handle(ctx context.Context, payload map[string]any) error {
	workspaceID, workspaceOK := payload["workspace_id"].(string)
	invitationID, invitationOK := payload["invitation_id"].(string)
	if !workspaceOK || !invitationOK || workspaceID == "" || invitationID == "" {
		return fmt.Errorf("invitation delivery payload is invalid")
	}
	if h.Invitations == nil || h.Sender == nil {
		return fmt.Errorf("invitation delivery dependencies are unavailable")
	}
	invitation, err := h.Invitations.Invitation(ctx, workspaceID, invitationID)
	if err != nil {
		return fmt.Errorf("load invitation for delivery: %w", err)
	}
	if invitation.Status != "pending" {
		return nil
	}
	if err = h.Sender.SendInvitation(ctx, invitation); err != nil {
		return fmt.Errorf("deliver invitation notification: %w", err)
	}
	return nil
}
