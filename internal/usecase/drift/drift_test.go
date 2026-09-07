package drift

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo/file"
	"context"
	"testing"
	"time"
)

func TestListReportsMissingAndChangedPackages(t *testing.T) {
	dir := t.TempDir()
	devices := file.NewDeviceRepo(dir)
	packages := file.NewPackageRepo(dir)
	now := time.Now().UTC()
	if err := packages.Save(context.Background(), entity.Release{Name: "skill", Version: "1.0.0", SHA256: "expected", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := devices.Save(context.Background(), entity.Device{ID: "device-1", Name: "laptop", InstalledPackages: map[string]string{"skill": "wrong"}}); err != nil {
		t.Fatal(err)
	}
	if err := devices.Save(context.Background(), entity.Device{ID: "device-2", Name: "desktop", InstalledPackages: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	items, err := New(devices, packages).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected two drift items, got %+v", items)
	}
}

func TestListForWorkspaceUsesManifestAsDesiredState(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	devices := file.NewDeviceRepo(dir)
	packages := file.NewPackageRepo(dir)
	manifests := file.NewWorkspaceRepo(dir)
	now := time.Now().UTC()
	if err := packages.SaveForWorkspace(ctx, "workspace-a", entity.Release{Name: "registry", Version: "1.0.0", SHA256: "registry", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := manifests.SaveManifest(ctx, entity.TeamManifest{WorkspaceID: "workspace-a", Revision: 1, Document: map[string]any{"packages": []any{map[string]any{"name": "manifest", "version": "2.0.0", "sha256": "wanted"}}}, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := devices.SaveForWorkspace(ctx, "workspace-a", entity.Device{ID: "device-a", Name: "laptop", InstalledPackages: map[string]string{"manifest": "old"}}); err != nil {
		t.Fatal(err)
	}
	items, err := New(devices, packages, manifests).ListForWorkspace(ctx, "workspace-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Package != "manifest" || items[0].ExpectedSHA256 != "wanted" {
		t.Fatalf("manifest drift was not used: %+v", items)
	}
}
