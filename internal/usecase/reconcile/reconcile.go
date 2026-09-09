package reconcile

import (
	"agentx/server/internal/apperror"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"fmt"
	"sort"
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
	deviceRepo, ok := s.devices.(repo.WorkspaceDeviceRepository)
	if !ok {
		return entity.ReconcilePlan{}, fmt.Errorf("workspace device persistence is unavailable")
	}
	devices, err := deviceRepo.ListForWorkspace(ctx, workspaceID)
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
		return entity.ReconcilePlan{}, apperror.New(apperror.KindNotFound, "device not found")
	}
	desired, err := entity.ParseDesiredManifest(manifest.Document)
	if err != nil {
		return entity.ReconcilePlan{}, fmt.Errorf("stored manifest is invalid: %w", err)
	}
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
	sort.Slice(actions, func(i, j int) bool {
		if actions[i].Package == actions[j].Package {
			return actions[i].Kind < actions[j].Kind
		}
		return actions[i].Package < actions[j].Package
	})
	return entity.ReconcilePlan{DeviceID: deviceID, Workspace: workspaceID, Revision: manifest.Revision, Actions: actions, Generated: time.Now().UTC()}, nil
}

func Desired(document map[string]any) ([]entity.DesiredPackage, error) {
	return entity.ParseDesiredManifest(document)
}
