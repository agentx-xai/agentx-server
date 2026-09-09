package legalhold

import (
	"agentx/server/internal/apperror"
	"agentx/server/internal/auth"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRepository struct {
	created  entity.LegalHold
	released entity.LegalHold
	err      error
}

func (r *fakeRepository) CreateLegalHold(_ context.Context, hold entity.LegalHold, _ entity.AuditEvent, _ entity.OutboxEvent) error {
	r.created = hold
	return r.err
}
func (r *fakeRepository) LegalHold(context.Context, string) (entity.LegalHold, error) {
	return entity.LegalHold{}, r.err
}
func (r *fakeRepository) ListLegalHoldsPage(context.Context, repo.LegalHoldFilter, repo.PageRequest) (repo.Page[entity.LegalHold], error) {
	return repo.Page[entity.LegalHold]{Items: []entity.LegalHold{r.created}, Total: 1}, r.err
}
func (r *fakeRepository) ReleaseLegalHold(_ context.Context, id, releasedBy, reason string, at time.Time, _ entity.AuditEvent, _ entity.OutboxEvent) (entity.LegalHold, error) {
	r.released = entity.LegalHold{ID: id, ReleasedBy: releasedBy, ReleasedAt: &at, ReleaseReason: reason}
	return r.released, r.err
}

func adminContext() context.Context {
	return auth.WithRequestID(auth.WithPrincipal(context.Background(), auth.Principal{UserID: "issuer|admin"}), "request-id")
}

func TestOnlyConfiguredComplianceAdministratorCanManageHolds(t *testing.T) {
	service := New(&fakeRepository{}, []string{"issuer|admin"})
	if _, err := service.Create(context.Background(), entity.LegalHoldTargetAccount, "issuer|user", "litigation"); kind(err) != apperror.KindForbidden {
		t.Fatalf("anonymous caller should be forbidden, got %v", err)
	}
	tenantOwner := auth.WithPrincipal(context.Background(), auth.Principal{UserID: "issuer|tenant-owner"})
	if _, err := service.Create(tenantOwner, entity.LegalHoldTargetAccount, "issuer|user", "litigation"); kind(err) != apperror.KindForbidden {
		t.Fatalf("tenant owner should be forbidden, got %v", err)
	}
	if _, err := service.Create(adminContext(), entity.LegalHoldTargetAccount, "issuer|user", "litigation"); err != nil {
		t.Fatal(err)
	}
}

func TestCreateAndReleaseValidation(t *testing.T) {
	repository := &fakeRepository{}
	service := New(repository, []string{"issuer|admin"})
	if _, err := service.Create(adminContext(), "other", "target", "reason"); kind(err) != apperror.KindValidation {
		t.Fatalf("invalid target type should fail validation, got %v", err)
	}
	if _, err := service.Create(adminContext(), entity.LegalHoldTargetWorkspace, "not-a-uuid", "reason"); kind(err) != apperror.KindValidation {
		t.Fatalf("invalid workspace ID should fail validation, got %v", err)
	}
	hold, err := service.Create(adminContext(), entity.LegalHoldTargetAccount, "issuer|user", "investigation")
	if err != nil || hold.CreatedBy != "issuer|admin" || repository.created.ID == "" {
		t.Fatalf("unexpected created hold: %+v %v", hold, err)
	}
	if _, err = service.Release(adminContext(), hold.ID, "release", "closed"); kind(err) != apperror.KindValidation {
		t.Fatalf("release must require exact confirmation, got %v", err)
	}
	released, err := service.Release(adminContext(), hold.ID, "RELEASE", "matter closed")
	if err != nil || released.ReleasedBy != "issuer|admin" || released.ReleaseReason != "matter closed" {
		t.Fatalf("unexpected release: %+v %v", released, err)
	}
}

func TestRepositoryErrorsAreMapped(t *testing.T) {
	repository := &fakeRepository{err: repo.NotFound("target")}
	service := New(repository, []string{"issuer|admin"})
	if _, err := service.Create(adminContext(), entity.LegalHoldTargetAccount, "issuer|missing", "reason"); kind(err) != apperror.KindNotFound {
		t.Fatalf("missing target should map to not found, got %v", err)
	}
	repository.err = repo.Conflict("duplicate")
	if _, err := service.Release(adminContext(), "00000000-0000-0000-0000-000000000001", "RELEASE", "reason"); kind(err) != apperror.KindConflict {
		t.Fatalf("release conflict should be public, got %v", err)
	}
}

func kind(err error) apperror.Kind {
	var applicationError *apperror.Error
	if errors.As(err, &applicationError) {
		return applicationError.Kind
	}
	return ""
}
