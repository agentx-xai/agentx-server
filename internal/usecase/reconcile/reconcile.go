package reconcile

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"fmt"
	"time"
)

type Service struct {
	devices  repo.DeviceRepository
	manifest repo.ManifestRepository
}

func New(devices repo.DeviceRepository, manifests repo.ManifestRepository) *Service {
	return &Service{devices: devices, manifest: manifests}
}

func (s *Service) Plan(ctx context.Context, workspaceID, deviceID string) (entity.ReconcilePlan, error) {
	if s.manifest == nil {
		return entity.ReconcilePlan{}, fmt.Errorf("manifest repository is unavailable")
	}
	manifest, err := s.manifest.CurrentManifest(ctx, workspaceID)
	if err != nil {
		return entity.ReconcilePlan{}, err
	}
	var devices []entity.Device
	if r, ok := s.devices.(repo.WorkspaceDeviceRepository); ok {
		devices, err = r.ListForWorkspace(ctx, workspaceID)
	} else {
		devices, err = s.devices.List(ctx)
	}
	if err != nil {
		return entity.ReconcilePlan{}, err
	}
	var device entity.Device
	for _, item := range devices {
		if item.ID == deviceID {
			device = item
			break
		}
	}
	if device.ID == "" {
		return entity.ReconcilePlan{}, fmt.Errorf("device not found")
	}
	desired := desiredPackages(manifest.Document)
	actions := make([]entity.ReconcileAction, 0)
	for _, item := range desired {
		observed := ""
		if device.InstalledPackages != nil {
			observed = device.InstalledPackages[item.Name]
		}
		if observed == "" {
			actions = append(actions, entity.ReconcileAction{Package: item.Name, Kind: "install", To: item.SHA256})
		} else if observed != item.SHA256 {
			actions = append(actions, entity.ReconcileAction{Package: item.Name, Kind: "update", From: observed, To: item.SHA256})
		}
	}
	wanted := map[string]bool{}
	for _, item := range desired {
		wanted[item.Name] = true
	}
	for name, observed := range device.InstalledPackages {
		if !wanted[name] {
			actions = append(actions, entity.ReconcileAction{Package: name, Kind: "remove", From: observed})
		}
	}
	return entity.ReconcilePlan{DeviceID: deviceID, Workspace: workspaceID, Revision: manifest.Revision, Actions: actions, Generated: time.Now().UTC()}, nil
}

func Desired(document map[string]any) []entity.DesiredPackage { return desiredPackages(document) }

func desiredPackages(document map[string]any) []entity.DesiredPackage {
	var out []entity.DesiredPackage
	for _, key := range []string{"packages", "skills"} {
		entries, ok := document[key].([]any)
		if !ok {
			continue
		}
		for _, raw := range entries {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name, _ := item["name"].(string)
			digest, _ := item["sha256"].(string)
			version, _ := item["version"].(string)
			if name != "" && digest != "" {
				out = append(out, entity.DesiredPackage{Name: name, Version: version, SHA256: digest})
			}
		}
	}
	return out
}
