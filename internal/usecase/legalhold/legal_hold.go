package legalhold

import (
	"agentx/server/internal/apperror"
	"agentx/server/internal/auth"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const maxReasonLength = 2000

type Service struct {
	repository repo.LegalHoldRepository
	admins     map[string]struct{}
}

func New(repository repo.LegalHoldRepository, adminIDs []string) *Service {
	admins := make(map[string]struct{}, len(adminIDs))
	for _, id := range adminIDs {
		admins[id] = struct{}{}
	}
	return &Service{repository: repository, admins: admins}
}

func (s *Service) Create(ctx context.Context, targetType, targetID, reason string) (entity.LegalHold, error) {
	principal, err := s.requireAdmin(ctx)
	if err != nil {
		return entity.LegalHold{}, err
	}
	targetType, targetID, reason = strings.TrimSpace(targetType), strings.TrimSpace(targetID), strings.TrimSpace(reason)
	if !validTargetType(targetType) || targetID == "" || len(targetID) > 2048 || reason == "" || len(reason) > maxReasonLength {
		return entity.LegalHold{}, apperror.New(apperror.KindValidation, "valid legal hold target and reason are required")
	}
	if targetType == entity.LegalHoldTargetWorkspace {
		if _, parseErr := uuid.Parse(targetID); parseErr != nil {
			return entity.LegalHold{}, apperror.New(apperror.KindValidation, "workspace target must be a UUID")
		}
	}
	now := time.Now().UTC()
	hold := entity.LegalHold{ID: uuid.NewString(), TargetType: targetType, TargetID: targetID, Reason: reason, CreatedBy: principal.UserID, CreatedAt: now}
	audit := entity.AuditEvent{Action: "legal_hold.create", ActorID: principal.UserID, ResourceType: "legal_hold", ResourceID: hold.ID, Metadata: map[string]any{"target_type": targetType, "target_id": targetID, "reason": reason}, RequestID: auth.RequestID(ctx), At: now}
	outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "legal_hold.created", Payload: map[string]any{"legal_hold_id": hold.ID, "target_type": targetType, "target_id": targetID}, AvailableAt: now}
	if err = s.repository.CreateLegalHold(ctx, hold, audit, outbox); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return entity.LegalHold{}, apperror.Wrap(apperror.KindNotFound, "legal hold target not found", err)
		}
		if errors.Is(err, repo.ErrConflict) {
			return entity.LegalHold{}, apperror.Wrap(apperror.KindConflict, "an active legal hold already exists for this target", err)
		}
		return entity.LegalHold{}, err
	}
	return hold, nil
}

func (s *Service) List(ctx context.Context, filter repo.LegalHoldFilter, request repo.PageRequest) (repo.Page[entity.LegalHold], error) {
	if _, err := s.requireAdmin(ctx); err != nil {
		return repo.Page[entity.LegalHold]{}, err
	}
	filter.Status, filter.TargetType, filter.TargetID = strings.TrimSpace(filter.Status), strings.TrimSpace(filter.TargetType), strings.TrimSpace(filter.TargetID)
	if filter.Status != "" && filter.Status != "active" && filter.Status != "released" {
		return repo.Page[entity.LegalHold]{}, apperror.New(apperror.KindValidation, "legal hold status must be active or released")
	}
	if filter.TargetType != "" && !validTargetType(filter.TargetType) {
		return repo.Page[entity.LegalHold]{}, apperror.New(apperror.KindValidation, "legal hold target type must be account or workspace")
	}
	return s.repository.ListLegalHoldsPage(ctx, filter, request)
}

func (s *Service) Release(ctx context.Context, id, confirmation, reason string) (entity.LegalHold, error) {
	principal, err := s.requireAdmin(ctx)
	if err != nil {
		return entity.LegalHold{}, err
	}
	id, reason = strings.TrimSpace(id), strings.TrimSpace(reason)
	if _, parseErr := uuid.Parse(id); parseErr != nil || confirmation != "RELEASE" || reason == "" || len(reason) > maxReasonLength {
		return entity.LegalHold{}, apperror.New(apperror.KindValidation, "RELEASE confirmation and a valid reason are required")
	}
	now := time.Now().UTC()
	audit := entity.AuditEvent{Action: "legal_hold.release", ActorID: principal.UserID, ResourceType: "legal_hold", ResourceID: id, Metadata: map[string]any{"reason": reason}, RequestID: auth.RequestID(ctx), At: now}
	outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "legal_hold.released", Payload: map[string]any{"legal_hold_id": id}, AvailableAt: now}
	hold, err := s.repository.ReleaseLegalHold(ctx, id, principal.UserID, reason, now, audit, outbox)
	if errors.Is(err, repo.ErrNotFound) {
		return entity.LegalHold{}, apperror.Wrap(apperror.KindNotFound, "legal hold not found", err)
	}
	if errors.Is(err, repo.ErrConflict) {
		return entity.LegalHold{}, apperror.Wrap(apperror.KindConflict, "legal hold is already released", err)
	}
	return hold, err
}

func (s *Service) requireAdmin(ctx context.Context) (auth.Principal, error) {
	principal, ok := auth.FromContext(ctx)
	if !ok || principal.UserID == "" {
		return auth.Principal{}, apperror.New(apperror.KindForbidden, "compliance administrator access required")
	}
	if _, ok = s.admins[principal.UserID]; !ok {
		return auth.Principal{}, apperror.New(apperror.KindForbidden, "compliance administrator access required")
	}
	return principal, nil
}

func validTargetType(value string) bool {
	return value == entity.LegalHoldTargetAccount || value == entity.LegalHoldTargetWorkspace
}
