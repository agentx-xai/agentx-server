package notification

import (
	"agentx/server/internal/entity"
	"strings"
	"testing"
	"time"
)

func TestRenderInvitationMessageContainsOnlyExpectedInvitationData(t *testing.T) {
	invitation := entity.WorkspaceInvitation{
		ID: "invitation-secret-not-in-message", Email: "invitee@example.test", WorkspaceName: "Example\r\nBcc: victim@example.test",
		Role: entity.RoleDeveloper, ExpiresAt: time.Date(2026, 9, 10, 1, 2, 3, 0, time.UTC),
	}
	message := string(renderInvitationMessage("notifications@example.test", "https://console.example.test", invitation, time.Date(2026, 9, 9, 1, 2, 3, 0, time.UTC)))
	for _, expected := range []string{"To: invitee@example.test", "AgentX workspace invitation", "Example Bcc: victim@example.test", "developer role", "https://console.example.test", "2026-09-10T01:02:03Z"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message missing %q: %s", expected, message)
		}
	}
	if strings.Contains(message, invitation.ID) || strings.Contains(message, "\r\nBcc: victim@example.test\r\n") {
		t.Fatalf("message leaked invitation identifier or injected a header: %s", message)
	}
}

func TestNewSMTPInvitationSenderRejectsUnsafeConfiguration(t *testing.T) {
	valid := SMTPConfig{Address: "smtp.example.test:465", From: "notifications@example.test", TLSMode: "tls", ConsoleURL: "https://console.example.test"}
	if _, err := NewSMTPInvitationSender(valid); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*SMTPConfig){
		func(c *SMTPConfig) { c.Address = "smtp.example.test" },
		func(c *SMTPConfig) { c.From = "AgentX <notifications@example.test>" },
		func(c *SMTPConfig) { c.TLSMode = "optional" },
	} {
		config := valid
		mutate(&config)
		if _, err := NewSMTPInvitationSender(config); err == nil {
			t.Fatalf("expected invalid SMTP config rejection: %+v", config)
		}
	}
}
