package file

import (
	"agentx/server/internal/entity"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

type DeviceRepo struct{ path string }

type storedDevice struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	entity.Device
}

func NewDeviceRepo(dir string) *DeviceRepo {
	return &DeviceRepo{path: filepath.Join(dir, "devices.json")}
}
func (r *DeviceRepo) List(_ context.Context) ([]entity.Device, error) {
	stored, err := r.load()
	if err != nil {
		return nil, err
	}
	out := make([]entity.Device, 0, len(stored))
	for _, item := range stored {
		out = append(out, item.Device)
	}
	return out, nil
}
func (r *DeviceRepo) Save(ctx context.Context, d entity.Device) error {
	stored, err := r.load()
	if err != nil {
		return err
	}
	for i, item := range stored {
		if item.WorkspaceID == "" && item.ID == d.ID && d.ID != "" {
			stored[i].Device = d
			return r.save(stored)
		}
	}
	return r.save(append(stored, storedDevice{Device: d}))
}
func (r *DeviceRepo) ListForWorkspace(_ context.Context, workspaceID string) ([]entity.Device, error) {
	stored, err := r.load()
	if err != nil {
		return nil, err
	}
	out := make([]entity.Device, 0)
	for _, item := range stored {
		if item.WorkspaceID == workspaceID {
			out = append(out, item.Device)
		}
	}
	return out, nil
}
func (r *DeviceRepo) SaveForWorkspace(_ context.Context, workspaceID string, d entity.Device) error {
	stored, err := r.load()
	if err != nil {
		return err
	}
	for i, item := range stored {
		if item.WorkspaceID == workspaceID && item.ID == d.ID && d.ID != "" {
			stored[i].Device = d
			return r.save(stored)
		}
	}
	return r.save(append(stored, storedDevice{WorkspaceID: workspaceID, Device: d}))
}
func (r *DeviceRepo) HeartbeatForWorkspace(_ context.Context, workspaceID, deviceID string, d entity.Device) error {
	stored, err := r.load()
	if err != nil {
		return err
	}
	for i, item := range stored {
		if item.WorkspaceID == workspaceID && item.ID == deviceID {
			stored[i].Device = d
			return r.save(stored)
		}
	}
	return os.ErrNotExist
}

func (r *DeviceRepo) load() ([]storedDevice, error) {
	b, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return []storedDevice{}, nil
	}
	if err != nil {
		return nil, err
	}
	var stored []storedDevice
	if err := json.Unmarshal(b, &stored); err != nil {
		return nil, err
	}
	return stored, nil
}

func (r *DeviceRepo) save(stored []storedDevice) error {
	b, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(r.path), 0o750); err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err = os.WriteFile(tmp, b, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

type AuditRepo struct{ path string }

type storedAuditEvent struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	entity.AuditEvent
}

func NewAuditRepo(dir string) *AuditRepo { return &AuditRepo{path: filepath.Join(dir, "audit.json")} }
func (r *AuditRepo) List(_ context.Context) ([]entity.AuditEvent, error) {
	stored, err := r.load()
	if err != nil {
		return nil, err
	}
	out := make([]entity.AuditEvent, 0, len(stored))
	for _, item := range stored {
		out = append(out, item.AuditEvent)
	}
	return out, nil
}
func (r *AuditRepo) Append(ctx context.Context, e entity.AuditEvent) error {
	stored, err := r.load()
	if err != nil {
		return err
	}
	return r.save(append(stored, storedAuditEvent{AuditEvent: e}))
}
func (r *AuditRepo) ListForWorkspace(_ context.Context, workspaceID string) ([]entity.AuditEvent, error) {
	stored, err := r.load()
	if err != nil {
		return nil, err
	}
	out := make([]entity.AuditEvent, 0)
	for _, item := range stored {
		if item.WorkspaceID == workspaceID {
			out = append(out, item.AuditEvent)
		}
	}
	return out, nil
}
func (r *AuditRepo) AppendForWorkspace(_ context.Context, workspaceID string, e entity.AuditEvent) error {
	stored, err := r.load()
	if err != nil {
		return err
	}
	return r.save(append(stored, storedAuditEvent{WorkspaceID: workspaceID, AuditEvent: e}))
}

func (r *AuditRepo) load() ([]storedAuditEvent, error) {
	b, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return []storedAuditEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	var stored []storedAuditEvent
	if err := json.Unmarshal(b, &stored); err != nil {
		return nil, err
	}
	return stored, nil
}

func (r *AuditRepo) save(stored []storedAuditEvent) error {
	b, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(r.path), 0o750); err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err = os.WriteFile(tmp, b, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}
