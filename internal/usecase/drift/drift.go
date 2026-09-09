package drift

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"fmt"
	"sort"
)

type Service struct {
	devices   repo.DeviceRepository
	packages  repo.PackageRepository
	manifests repo.ManifestRepository
}

func New(d repo.DeviceRepository, p repo.PackageRepository, manifests ...repo.ManifestRepository) *Service {
	var manifestRepo repo.ManifestRepository
	if len(manifests) > 0 {
		manifestRepo = manifests[0]
	}
	return &Service{devices: d, packages: p, manifests: manifestRepo}
}
func (s *Service) List(ctx context.Context) ([]entity.DriftItem, error) {
	devices, err := s.devices.List(ctx)
	if err != nil {
		return nil, err
	}
	releases, err := s.packages.List(ctx)
	if err != nil {
		return nil, err
	}
	return calculate(devices, releases), nil
}
func (s *Service) calculateForWorkspace(ctx context.Context, workspaceID string, devices []entity.Device, releases []entity.Release) ([]entity.DriftItem, error) {
	if s.manifests != nil {
		manifest, err := s.manifests.CurrentManifest(ctx, workspaceID)
		if err != nil {
			return nil, err
		}
		if manifest.Revision > 0 {
			desired, parseErr := entity.ParseDesiredManifest(manifest.Document)
			if parseErr != nil {
				return nil, fmt.Errorf("stored manifest is invalid: %w", parseErr)
			}
			expected := make([]entity.Release, 0, len(desired))
			for _, item := range desired {
				expected = append(expected, entity.Release{Name: item.Name, Version: item.Version, SHA256: item.SHA256})
			}
			return calculate(devices, expected), nil
		}
	}
	return calculate(devices, releases), nil
}
func (s *Service) ListForWorkspace(ctx context.Context, workspaceID string) ([]entity.DriftItem, error) {
	deviceRepo, ok := s.devices.(repo.WorkspaceDeviceRepository)
	if !ok {
		return nil, fmt.Errorf("workspace device persistence is unavailable")
	}
	devices, err := deviceRepo.ListForWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	packageRepo, ok := s.packages.(repo.WorkspacePackageRepository)
	if !ok {
		return nil, fmt.Errorf("workspace package persistence is unavailable")
	}
	releases, err := packageRepo.ListForWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return s.calculateForWorkspace(ctx, workspaceID, devices, releases)
}
func calculate(devices []entity.Device, releases []entity.Release) []entity.DriftItem {
	expected := map[string]entity.Release{}
	for _, r := range releases {
		if old, ok := expected[r.Name]; !ok || r.CreatedAt.After(old.CreatedAt) {
			expected[r.Name] = r
		}
	}
	var out []entity.DriftItem
	for _, d := range devices {
		for name, want := range expected {
			got := d.InstalledPackages[name]
			if got == "" {
				out = append(out, entity.DriftItem{DeviceID: d.ID, DeviceName: d.Name, Package: name, ExpectedSHA256: want.SHA256, Kind: "missing"})
			} else if got != want.SHA256 {
				out = append(out, entity.DriftItem{DeviceID: d.ID, DeviceName: d.Name, Package: name, ExpectedSHA256: want.SHA256, ObservedSHA256: got, Kind: "changed"})
			}
		}
		for name, observed := range d.InstalledPackages {
			if _, ok := expected[name]; !ok {
				out = append(out, entity.DriftItem{DeviceID: d.ID, DeviceName: d.Name, Package: name, ObservedSHA256: observed, Kind: "extra"})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DeviceID != out[j].DeviceID {
			return out[i].DeviceID < out[j].DeviceID
		}
		if out[i].Package != out[j].Package {
			return out[i].Package < out[j].Package
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}
