package drift

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"fmt"
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
		if expected := expectedFromManifest(manifest.Document); len(expected) > 0 {
			return calculate(devices, expected), nil
		}
	}
	return calculate(devices, releases), nil
}

func expectedFromManifest(document map[string]any) []entity.Release {
	var expected []entity.Release
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
				expected = append(expected, entity.Release{Name: name, Version: version, SHA256: digest})
			}
		}
	}
	return expected
}
func (s *Service) ListForWorkspace(ctx context.Context, workspaceID string) ([]entity.DriftItem, error) {
	var devices []entity.Device
	var releases []entity.Release
	var err error
	if r, ok := s.devices.(repo.WorkspaceDeviceRepository); ok {
		devices, err = r.ListForWorkspace(ctx, workspaceID)
	} else {
		return nil, fmt.Errorf("workspace device repository is unavailable")
	}
	if err != nil {
		return nil, err
	}
	if r, ok := s.packages.(repo.WorkspacePackageRepository); ok {
		releases, err = r.ListForWorkspace(ctx, workspaceID)
	} else {
		return nil, fmt.Errorf("workspace package repository is unavailable")
	}
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
	}
	return out
}
