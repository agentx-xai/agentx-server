package account

import (
	"agentx/server/internal/apperror"
	"agentx/server/internal/auth"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repo repo.AccountRepository
}

func New(repository repo.AccountRepository) *Service {
	return &Service{repo: repository}
}

func principal(ctx context.Context) (auth.Principal, error) {
	value, ok := auth.FromContext(ctx)
	if !ok || strings.TrimSpace(value.UserID) == "" || value.UserID == "anonymous" {
		return auth.Principal{}, apperror.New(apperror.KindForbidden, "authenticated account is required")
	}
	return value, nil
}

func verifiedEmail(value auth.Principal) string {
	if !value.EmailVerified {
		return ""
	}
	email := strings.TrimSpace(value.Email)
	address, err := mail.ParseAddress(email)
	if err != nil || address.Name != "" || !strings.EqualFold(address.Address, email) {
		return ""
	}
	return strings.ToLower(address.Address)
}

func (s *Service) Export(ctx context.Context) (entity.AccountExport, error) {
	value, err := principal(ctx)
	if err != nil {
		return entity.AccountExport{}, err
	}
	export, err := s.repo.ExportAccount(ctx, value.UserID, verifiedEmail(value))
	if err != nil {
		return entity.AccountExport{}, err
	}
	export.SchemaVersion = entity.AccountExportSchemaVersion
	export.ExportedAt = time.Now().UTC()
	if export.User.ID == "" {
		export.User = entity.User{ID: value.UserID, Issuer: value.Issuer, Subject: value.Subject, Email: value.Email, EmailVerified: value.EmailVerified}
	}
	if export.Memberships == nil {
		export.Memberships = []entity.AccountMembership{}
	}
	if export.Invitations == nil {
		export.Invitations = []entity.WorkspaceInvitation{}
	}
	if export.AuditEvents == nil {
		export.AuditEvents = []entity.AccountAuditEvent{}
	}
	return export, nil
}

func (s *Service) Delete(ctx context.Context, confirmation string) error {
	value, err := principal(ctx)
	if err != nil {
		return err
	}
	if confirmation != "DELETE" {
		return apperror.New(apperror.KindValidation, "confirmation must equal DELETE")
	}
	err = s.repo.DeleteAccount(ctx, entity.AccountDeletion{
		UserID: value.UserID, Email: verifiedEmail(value), DeletionID: uuid.NewString(),
		RequestID: auth.RequestID(ctx), At: time.Now().UTC(),
	})
	if errors.Is(err, repo.ErrConflict) {
		if strings.Contains(err.Error(), "active legal hold") {
			return apperror.Wrap(apperror.KindConflict, "account deletion is blocked by an active legal hold", err)
		}
		return apperror.Wrap(apperror.KindConflict, "delete or transfer owned workspaces before deleting the account", err)
	}
	return err
}
