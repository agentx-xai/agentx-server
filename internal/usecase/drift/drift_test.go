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

func TestWorkspaceDriftUsesManifestDesiredState(t *testing.T) {
	dir := t.TempDir()
	devices := file.NewDeviceRepo(dir)
	packages := file.NewPackageRepo(dir)
	manifests := file.NewWorkspaceRepo(dir)
	ctx := context.Background()
	workspaceID := "workspace-1"
	if err := devices.SaveForWorkspace(ctx, workspaceID, entity.Device{ID: "device-1", Name: "laptop", InstalledPackages: map[string]string{"skill": "installed"}}); err != nil {
		t.Fatal(err)
	}
	if err := packages.SaveForWorkspace(ctx, workspaceID, entity.Release{Name: "skill", Version: "2.0.0", SHA256: "latest", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	desired := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := manifests.SaveManifest(ctx, entity.TeamManifest{ID: "manifest-1", WorkspaceID: workspaceID, Revision: 1, Document: map[string]any{"version": 1, "packages": []any{map[string]any{"name": "skill", "version": "1.0.0", "sha256": desired}}}}); err != nil {
		t.Fatal(err)
	}
	items, err := New(devices, packages, manifests).ListForWorkspace(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ExpectedSHA256 != desired || items[0].ObservedSHA256 != "installed" {
		t.Fatalf("workspace drift did not use manifest state: %+v", items)
	}
}

func TestWorkspaceDriftTreatsSavedEmptyManifestAsEmptyDesiredState(t *testing.T) {
	dir := t.TempDir()
	devices := file.NewDeviceRepo(dir)
	packages := file.NewPackageRepo(dir)
	manifests := file.NewWorkspaceRepo(dir)
	ctx := context.Background()
	workspaceID := "workspace-1"
	if err := devices.SaveForWorkspace(ctx, workspaceID, entity.Device{ID: "device-1", Name: "laptop", InstalledPackages: map[string]string{"extra": "observed"}}); err != nil {
		t.Fatal(err)
	}
	if err := packages.SaveForWorkspace(ctx, workspaceID, entity.Release{Name: "fallback", Version: "1.0.0", SHA256: "fallback", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := manifests.SaveManifest(ctx, entity.TeamManifest{ID: "manifest-1", WorkspaceID: workspaceID, Revision: 1, Document: map[string]any{"version": 1, "packages": []any{}}}); err != nil {
		t.Fatal(err)
	}
	items, err := New(devices, packages, manifests).ListForWorkspace(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind != "extra" || items[0].Package != "extra" || items[0].ExpectedSHA256 != "" {
		t.Fatalf("saved empty manifest did not report extra package: %+v", items)
	}
}
