package device

import (
	"agentx/server/internal/auth"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	devices repo.DeviceRepository
	audit   repo.AuditRepository
	outbox  repo.OutboxRepository
}

func New(d repo.DeviceRepository, a repo.AuditRepository) *Service {
	return &Service{devices: d, audit: a}
}
func (s *Service) SetOutbox(w repo.OutboxRepository) { s.outbox = w }
func auditEvent(ctx context.Context, event entity.AuditEvent) entity.AuditEvent {
	if principal, ok := auth.FromContext(ctx); ok {
		event.ActorID = principal.UserID
	}
	event.RequestID = auth.RequestID(ctx)
	return event
}
func (s *Service) List(ctx context.Context) ([]entity.Device, error) { return s.devices.List(ctx) }
func (s *Service) ListForWorkspace(ctx context.Context, workspaceID string) ([]entity.Device, error) {
	if r, ok := s.devices.(repo.WorkspaceDeviceRepository); ok {
		return r.ListForWorkspace(ctx, workspaceID)
	}
	return s.List(ctx)
}
func (s *Service) Register(ctx context.Context, d entity.Device) (entity.Device, error) {
	if strings.TrimSpace(d.Name) == "" {
		return d, errors.New("device name is required")
	}
	if strings.TrimSpace(d.ID) == "" {
		d.ID = uuid.NewString()
	}
	d.UpdatedAt = time.Now().UTC()
	if err := s.devices.Save(ctx, d); err != nil {
		return d, err
	}
	_ = s.audit.Append(ctx, auditEvent(ctx, entity.AuditEvent{Action: "device.register", DeviceID: d.ID, ResourceType: "device", ResourceID: d.ID, At: d.UpdatedAt}))
	return d, nil
}
func (s *Service) RegisterForWorkspace(ctx context.Context, workspaceID string, d entity.Device) (entity.Device, error) {
	if strings.TrimSpace(d.Name) == "" {
		return d, errors.New("device name is required")
	}
	if strings.TrimSpace(d.ID) == "" {
		d.ID = uuid.NewString()
	}
	d.UpdatedAt = time.Now().UTC()
	if r, ok := s.devices.(repo.WorkspaceDeviceRepository); ok {
		if err := r.SaveForWorkspace(ctx, workspaceID, d); err != nil {
			return d, err
		}
		if writer, ok := s.audit.(repo.WorkspaceAuditWriter); ok {
			_ = writer.AppendForWorkspace(ctx, workspaceID, auditEvent(ctx, entity.AuditEvent{Action: "device.register", DeviceID: d.ID, ResourceType: "device", ResourceID: d.ID, At: d.UpdatedAt}))
		}
		s.enqueue(ctx, workspaceID, "device.registered", map[string]any{"workspace_id": workspaceID, "device_id": d.ID})
		return d, nil
	}
	return s.Register(ctx, d)
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
	return errors.New("device not found")
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
		return errors.New("device not found")
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
	if heartbeat, ok := s.devices.(repo.WorkspaceDeviceHeartbeat); ok {
		if err := heartbeat.HeartbeatForWorkspace(ctx, workspaceID, id, d); err != nil {
			return err
		}
	} else {
		return errors.New("workspace heartbeat is unavailable")
	}
	if writer, ok := s.audit.(repo.WorkspaceAuditWriter); ok {
		_ = writer.AppendForWorkspace(ctx, workspaceID, auditEvent(ctx, entity.AuditEvent{Action: "device.heartbeat", DeviceID: id, ResourceType: "device", ResourceID: id, At: d.UpdatedAt}))
	}
	s.enqueue(ctx, workspaceID, "device.heartbeat", map[string]any{"workspace_id": workspaceID, "device_id": id})
	return nil
}

func (s *Service) enqueue(ctx context.Context, workspaceID, topic string, payload map[string]any) {
	if s.outbox == nil {
		return
	}
	_ = s.outbox.Enqueue(ctx, entity.OutboxEvent{ID: uuid.NewString(), Topic: topic, Payload: payload, AvailableAt: time.Now().UTC()})
}
func (s *Service) Audit(ctx context.Context) ([]entity.AuditEvent, error) { return s.audit.List(ctx) }
func (s *Service) AuditForWorkspace(ctx context.Context, workspaceID string) ([]entity.AuditEvent, error) {
	if r, ok := s.audit.(repo.WorkspaceAuditRepository); ok {
		return r.ListForWorkspace(ctx, workspaceID)
	}
	return s.Audit(ctx)
}
