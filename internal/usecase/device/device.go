package device

import (
	"agentx/server/internal/apperror"
	"agentx/server/internal/auth"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	devices repo.DeviceRepository
	audit   repo.AuditRepository
	outbox  repo.OutboxRepository
	uow     repo.UnitOfWork

	idempotencyMu sync.Mutex
	idempotency   map[string]entity.DeviceIdempotencyRecord
}

func New(d repo.DeviceRepository, a repo.AuditRepository) *Service {
	return &Service{devices: d, audit: a, idempotency: map[string]entity.DeviceIdempotencyRecord{}}
}
func (s *Service) SetOutbox(w repo.OutboxRepository) { s.outbox = w }
func (s *Service) SetUnitOfWork(uow repo.UnitOfWork) { s.uow = uow }
func auditEvent(ctx context.Context, event entity.AuditEvent) entity.AuditEvent {
	if principal, ok := auth.FromContext(ctx); ok {
		event.ActorID = principal.UserID
	}
	event.RequestID = auth.RequestID(ctx)
	return event
}
func (s *Service) List(ctx context.Context) ([]entity.Device, error) { return s.devices.List(ctx) }
func (s *Service) ListPage(ctx context.Context, request repo.PageRequest) (repo.Page[entity.Device], error) {
	if pager, ok := s.devices.(repo.DevicePageRepository); ok {
		return pager.ListPage(ctx, request)
	}
	items, err := s.List(ctx)
	if err != nil {
		return repo.Page[entity.Device]{}, err
	}
	return repo.PageSlice(items, request), nil
}
func (s *Service) ListForWorkspace(ctx context.Context, workspaceID string) ([]entity.Device, error) {
	if r, ok := s.devices.(repo.WorkspaceDeviceRepository); ok {
		return r.ListForWorkspace(ctx, workspaceID)
	}
	return nil, errors.New("workspace device persistence is unavailable")
}
func (s *Service) ListForWorkspacePage(ctx context.Context, workspaceID string, request repo.PageRequest) (repo.Page[entity.Device], error) {
	if pager, ok := s.devices.(repo.WorkspaceDevicePageRepository); ok {
		return pager.ListPageForWorkspace(ctx, workspaceID, request)
	}
	items, err := s.ListForWorkspace(ctx, workspaceID)
	if err != nil {
		return repo.Page[entity.Device]{}, err
	}
	return repo.PageSlice(items, request), nil
}
func (s *Service) Register(ctx context.Context, d entity.Device) (entity.Device, error) {
	if strings.TrimSpace(d.Name) == "" {
		return d, apperror.New(apperror.KindValidation, "device name is required")
	}
	if strings.TrimSpace(d.ID) == "" {
		d.ID = uuid.NewString()
	}
	d.UpdatedAt = time.Now().UTC()
	if err := s.devices.Save(ctx, d); err != nil {
		return d, err
	}
	if s.audit != nil {
		if err := s.audit.Append(ctx, auditEvent(ctx, entity.AuditEvent{Action: "device.register", DeviceID: d.ID, ResourceType: "device", ResourceID: d.ID, At: d.UpdatedAt})); err != nil {
			return d, fmt.Errorf("device committed but audit append failed: %w", err)
		}
	}
	return d, nil
}
func (s *Service) RegisterForWorkspace(ctx context.Context, workspaceID string, d entity.Device) (entity.Device, error) {
	d, err := prepareDevice(d)
	if err != nil {
		return d, err
	}
	return d, s.registerWorkspaceMutation(ctx, workspaceID, d)
}

func (s *Service) RegisterForWorkspaceIdempotent(ctx context.Context, workspaceID, key string, d entity.Device) (entity.Device, bool, error) {
	if strings.TrimSpace(key) == "" {
		return d, false, apperror.New(apperror.KindValidation, "idempotency key is required")
	}
	if len(key) > 255 {
		return d, false, apperror.New(apperror.KindValidation, "idempotency key must be at most 255 bytes")
	}
	if strings.TrimSpace(d.Name) == "" {
		return d, false, apperror.New(apperror.KindValidation, "device name is required")
	}
	fingerprint, err := deviceFingerprint(d)
	if err != nil {
		return d, false, err
	}
	if strings.TrimSpace(d.ID) == "" {
		d.ID = uuid.NewString()
	}
	d.UpdatedAt = time.Now().UTC()
	audit := auditEvent(ctx, entity.AuditEvent{Action: "device.register", DeviceID: d.ID, ResourceType: "device", ResourceID: d.ID, At: d.UpdatedAt})
	outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "device.registered", Payload: map[string]any{"workspace_id": workspaceID, "device_id": d.ID}, AvailableAt: d.UpdatedAt}
	record := entity.DeviceIdempotencyRecord{Fingerprint: fingerprint, Device: d}
	if repository, ok := s.devices.(repo.AtomicDeviceIdempotencyRepository); ok {
		saved, replayed, err := repository.SaveForWorkspaceIdempotent(ctx, workspaceID, key, record, audit, outbox)
		if errors.Is(err, repo.ErrIdempotencyConflict) {
			err = apperror.Wrap(apperror.KindConflict, "idempotency key already used for a different request", err)
		}
		return saved, replayed, err
	}

	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	mapKey := workspaceID + "\x00" + key
	if existing, found := s.idempotency[mapKey]; found {
		if existing.Fingerprint != fingerprint {
			return entity.Device{}, false, apperror.New(apperror.KindConflict, "idempotency key already used for a different request")
		}
		return existing.Device, true, nil
	}
	if err := s.registerWorkspaceMutation(ctx, workspaceID, d); err != nil {
		return d, false, err
	}
	s.idempotency[mapKey] = record
	return d, false, nil
}

func prepareDevice(d entity.Device) (entity.Device, error) {
	if strings.TrimSpace(d.Name) == "" {
		return d, apperror.New(apperror.KindValidation, "device name is required")
	}
	if strings.TrimSpace(d.ID) == "" {
		d.ID = uuid.NewString()
	}
	d.UpdatedAt = time.Now().UTC()
	return d, nil
}

func deviceFingerprint(d entity.Device) (string, error) {
	request := struct {
		ID                string            `json:"id"`
		Name              string            `json:"name"`
		Agent             string            `json:"agent"`
		Status            string            `json:"status"`
		InstalledPackages map[string]string `json:"installed_packages,omitempty"`
	}{d.ID, d.Name, d.Agent, d.Status, d.InstalledPackages}
	raw, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

func (s *Service) registerWorkspaceMutation(ctx context.Context, workspaceID string, d entity.Device) error {
	r, ok := s.devices.(repo.WorkspaceDeviceRepository)
	if !ok {
		return errors.New("workspace device persistence is unavailable")
	}
	audit := auditEvent(ctx, entity.AuditEvent{Action: "device.register", DeviceID: d.ID, ResourceType: "device", ResourceID: d.ID, At: d.UpdatedAt})
	outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "device.registered", Payload: map[string]any{"workspace_id": workspaceID, "device_id": d.ID}, AvailableAt: d.UpdatedAt}
	return s.mutateWorkspace(ctx, workspaceID, func(txCtx context.Context) error { return r.SaveForWorkspace(txCtx, workspaceID, d) }, audit, outbox)
}
func (s *Service) Heartbeat(ctx context.Context, id string) error {
	v, e := s.devices.List(ctx)
	if e != nil {
		return e
	}
	for _, d := range v {
		if d.ID == id {
			d.Status = "online"
			d.UpdatedAt = time.Now().UTC()
			return s.devices.Save(ctx, d)
		}
	}
	return apperror.New(apperror.KindNotFound, "device not found")
}
func (s *Service) HeartbeatForWorkspace(ctx context.Context, workspaceID, id string, d entity.Device) error {
	current, err := s.ListForWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	var found entity.Device
	for _, item := range current {
		if item.ID == id {
			found = item
			break
		}
	}
	if found.ID == "" {
		return apperror.New(apperror.KindNotFound, "device not found")
	}
	if d.Name == "" {
		d.Name = found.Name
	}
	if d.Agent == "" {
		d.Agent = found.Agent
	}
	if d.InstalledPackages == nil {
		d.InstalledPackages = found.InstalledPackages
	}
	d.ID = id
	d.Status = "online"
	d.UpdatedAt = time.Now().UTC()
	heartbeat, ok := s.devices.(repo.WorkspaceDeviceHeartbeat)
	if !ok {
		return errors.New("workspace heartbeat is unavailable")
	}
	audit := auditEvent(ctx, entity.AuditEvent{Action: "device.heartbeat", DeviceID: id, ResourceType: "device", ResourceID: id, At: d.UpdatedAt})
	outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "device.heartbeat", Payload: map[string]any{"workspace_id": workspaceID, "device_id": id}, AvailableAt: d.UpdatedAt}
	return s.mutateWorkspace(ctx, workspaceID, func(txCtx context.Context) error { return heartbeat.HeartbeatForWorkspace(txCtx, workspaceID, id, d) }, audit, outbox)
}

func (s *Service) mutateWorkspace(ctx context.Context, workspaceID string, change func(context.Context) error, audit entity.AuditEvent, outbox entity.OutboxEvent) error {
	operation := func(txCtx context.Context) error {
		if err := change(txCtx); err != nil {
			return err
		}
		if writer, ok := s.audit.(repo.WorkspaceAuditWriter); ok {
			if err := writer.AppendForWorkspace(txCtx, workspaceID, audit); err != nil {
				return err
			}
		}
		if s.outbox != nil {
			return s.outbox.Enqueue(txCtx, outbox)
		}
		return nil
	}
	if s.uow != nil {
		return s.uow.WithinTransaction(ctx, operation)
	}
	return operation(ctx)
}
func (s *Service) Audit(ctx context.Context) ([]entity.AuditEvent, error) { return s.audit.List(ctx) }
func (s *Service) AuditPage(ctx context.Context, request repo.PageRequest) (repo.Page[entity.AuditEvent], error) {
	if pager, ok := s.audit.(repo.AuditPageRepository); ok {
		return pager.ListPage(ctx, request)
	}
	items, err := s.Audit(ctx)
	if err != nil {
		return repo.Page[entity.AuditEvent]{}, err
	}
	return repo.PageSlice(items, request), nil
}
func (s *Service) AuditForWorkspace(ctx context.Context, workspaceID string) ([]entity.AuditEvent, error) {
	if r, ok := s.audit.(repo.WorkspaceAuditRepository); ok {
		return r.ListForWorkspace(ctx, workspaceID)
	}
	return nil, errors.New("workspace audit persistence is unavailable")
}
func (s *Service) AuditForWorkspacePage(ctx context.Context, workspaceID string, request repo.PageRequest) (repo.Page[entity.AuditEvent], error) {
	if pager, ok := s.audit.(repo.WorkspaceAuditPageRepository); ok {
		return pager.ListPageForWorkspace(ctx, workspaceID, request)
	}
	items, err := s.AuditForWorkspace(ctx, workspaceID)
	if err != nil {
		return repo.Page[entity.AuditEvent]{}, err
	}
	return repo.PageSlice(items, request), nil
}
