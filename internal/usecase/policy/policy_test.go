package policy

import (
	"agentx/server/internal/apperror"
	"agentx/server/internal/repo/file"
	"agentx/server/internal/usecase/workspace"
	"context"
	"testing"
)

func policyFixture(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	repository := file.NewWorkspaceRepo(dir)
	workspaces := workspace.New(repository)
	created, err := workspaces.Create(context.Background(), "Platform", "platform")
	if err != nil {
		t.Fatal(err)
	}
	return New(repository, workspaces), created.ID
}

func requireValidation(t *testing.T, err error) {
	t.Helper()
	if kind, _, ok := apperror.Public(err); !ok || kind != apperror.KindValidation {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestReplaceRejectsUnsupportedAndNonBooleanSettings(t *testing.T) {
	service, workspaceID := policyFixture(t)
	_, err := service.Replace(context.Background(), workspaceID, map[string]any{"unknown": true})
	requireValidation(t, err)
	_, err = service.Replace(context.Background(), workspaceID, map[string]any{"require_approval": "yes"})
	requireValidation(t, err)
	current, currentErr := service.Current(context.Background(), workspaceID)
	if currentErr != nil || current.Revision != 0 {
		t.Fatalf("invalid policy was persisted: %+v %v", current, currentErr)
	}
}

func TestReplaceRejectsUnavailableSignatureVerification(t *testing.T) {
	service, workspaceID := policyFixture(t)
	_, err := service.Replace(context.Background(), workspaceID, map[string]any{"require_signature": true})
	requireValidation(t, err)
	service.SetSignatureVerificationAvailable(true)
	stored, err := service.Replace(context.Background(), workspaceID, map[string]any{
		"require_signature": true,
		"require_approval":  true,
	})
	if err != nil || stored.Revision != 1 {
		t.Fatalf("valid policy was rejected: %+v %v", stored, err)
	}
}
