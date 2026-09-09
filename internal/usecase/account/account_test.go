package account

import (
	"agentx/server/internal/auth"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRepository struct {
	export    entity.AccountExport
	deleted   entity.AccountDeletion
	deleteErr error
}

func (r *fakeRepository) ExportAccount(context.Context, string, string) (entity.AccountExport, error) {
	return r.export, nil
}

func (r *fakeRepository) DeleteAccount(_ context.Context, deletion entity.AccountDeletion) error {
	r.deleted = deletion
	return r.deleteErr
}

func TestExportUsesStoredAccountDataAndStableCollections(t *testing.T) {
	createdAt := time.Now().UTC().Add(-time.Hour)
	repository := &fakeRepository{export: entity.AccountExport{User: entity.User{ID: "issuer|subject", Issuer: "issuer", Subject: "subject", Email: "stored@example.com", EmailVerified: true, CreatedAt: createdAt}}}
	service := New(repository)
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{UserID: "issuer|subject", Issuer: "issuer", Subject: "subject", Email: "current@example.com", EmailVerified: true})
	export, err := service.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if export.SchemaVersion != 1 || export.ExportedAt.IsZero() || export.User.Email != "stored@example.com" || !export.User.CreatedAt.Equal(createdAt) {
		t.Fatalf("unexpected export: %+v", export)
	}
	if export.Memberships == nil || export.Invitations == nil || export.AuditEvents == nil {
		t.Fatalf("export collections must be arrays: %+v", export)
	}
}

func TestDeleteRequiresConfirmationAndMapsOwnerConflict(t *testing.T) {
	repository := &fakeRepository{}
	service := New(repository)
	ctx := auth.WithRequestID(auth.WithPrincipal(context.Background(), auth.Principal{UserID: "issuer|subject", Email: "Person@Example.com", EmailVerified: true}), "request-id")
	if err := service.Delete(ctx, "delete"); err == nil {
		t.Fatal("lowercase confirmation was accepted")
	}
	if repository.deleted.UserID != "" {
		t.Fatal("repository was called before confirmation")
	}
	if err := service.Delete(ctx, "DELETE"); err != nil {
		t.Fatal(err)
	}
	if repository.deleted.UserID != "issuer|subject" || repository.deleted.Email != "person@example.com" || repository.deleted.RequestID != "request-id" || repository.deleted.DeletionID == "" {
		t.Fatalf("unexpected deletion command: %+v", repository.deleted)
	}
	repository.deleteErr = repo.Conflict("account owns a workspace")
	if err := service.Delete(ctx, "DELETE"); err == nil || !errors.Is(err, repo.ErrConflict) {
		t.Fatalf("owner conflict was not preserved: %v", err)
	}
	repository.deleteErr = repo.Conflict("account is under an active legal hold")
	if err := service.Delete(ctx, "DELETE"); err == nil || err.Error() != "account deletion is blocked by an active legal hold" {
		t.Fatalf("legal hold conflict was not mapped: %v", err)
	}
}

func TestAccountOperationsRequireAuthentication(t *testing.T) {
	service := New(&fakeRepository{})
	if _, err := service.Export(context.Background()); err == nil {
		t.Fatal("anonymous export was accepted")
	}
	if err := service.Delete(context.Background(), "DELETE"); err == nil {
		t.Fatal("anonymous deletion was accepted")
	}
}
