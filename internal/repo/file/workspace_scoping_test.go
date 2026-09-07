package file

import (
	"agentx/server/internal/entity"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceRepositoriesDoNotCrossRead(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	now := time.Now().UTC()

	packages := NewPackageRepo(dir)
	first := entity.Release{Name: "shared", Version: "1.0.0", SHA256: "first", CreatedAt: now}
	second := entity.Release{Name: "shared", Version: "1.0.0", SHA256: "second", CreatedAt: now}
	if err := packages.SaveForWorkspace(ctx, "workspace-a", first); err != nil {
		t.Fatal(err)
	}
	if err := packages.SaveForWorkspace(ctx, "workspace-b", second); err != nil {
		t.Fatal(err)
	}
	gotA, err := packages.ListForWorkspace(ctx, "workspace-a")
	if err != nil || len(gotA) != 1 || gotA[0].SHA256 != "first" {
		t.Fatalf("workspace-a packages: %+v %v", gotA, err)
	}
	gotB, err := packages.ListForWorkspace(ctx, "workspace-b")
	if err != nil || len(gotB) != 1 || gotB[0].SHA256 != "second" {
		t.Fatalf("workspace-b packages: %+v %v", gotB, err)
	}

	devices := NewDeviceRepo(dir)
	if err := devices.SaveForWorkspace(ctx, "workspace-a", entity.Device{ID: "device-1", Name: "A", UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := devices.SaveForWorkspace(ctx, "workspace-b", entity.Device{ID: "device-1", Name: "B", UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	gotDevices, err := devices.ListForWorkspace(ctx, "workspace-a")
	if err != nil || len(gotDevices) != 1 || gotDevices[0].Name != "A" {
		t.Fatalf("workspace-a devices: %+v %v", gotDevices, err)
	}

	audit := NewAuditRepo(dir)
	if err := audit.AppendForWorkspace(ctx, "workspace-a", entity.AuditEvent{Action: "one", At: now}); err != nil {
		t.Fatal(err)
	}
	if err := audit.AppendForWorkspace(ctx, "workspace-b", entity.AuditEvent{Action: "two", At: now}); err != nil {
		t.Fatal(err)
	}
	gotAudit, err := audit.ListForWorkspace(ctx, "workspace-a")
	if err != nil || len(gotAudit) != 1 || gotAudit[0].Action != "one" {
		t.Fatalf("workspace-a audit: %+v %v", gotAudit, err)
	}
}

func TestWorkspaceRepositoriesReadLegacyFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "packages.json"), []byte(`[{"name":"legacy","version":"1.0.0","sha256":"old"}]`), 0640); err != nil {
		t.Fatal(err)
	}
	packages, err := NewPackageRepo(dir).List(context.Background())
	if err != nil || len(packages) != 1 || packages[0].Name != "legacy" {
		t.Fatalf("legacy packages: %+v %v", packages, err)
	}
	if scoped, err := NewPackageRepo(dir).ListForWorkspace(context.Background(), "workspace-a"); err != nil || len(scoped) != 0 {
		t.Fatalf("legacy package leaked into workspace: %+v %v", scoped, err)
	}
}

func TestDeleteWorkspaceCleansWorkspaceOwnedFiles(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	now := time.Now().UTC()
	workspaces := NewWorkspaceRepo(dir)
	if err := workspaces.Create(ctx, entity.Workspace{ID: "workspace-a", Name: "A", Slug: "workspace-a", CreatedAt: now}, entity.Membership{WorkspaceID: "workspace-a", UserID: "owner", Role: entity.RoleOwner, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := workspaces.Create(ctx, entity.Workspace{ID: "workspace-b", Name: "B", Slug: "workspace-b", CreatedAt: now}, entity.Membership{WorkspaceID: "workspace-b", UserID: "owner", Role: entity.RoleOwner, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := NewPackageRepo(dir).SaveForWorkspace(ctx, "workspace-a", entity.Release{Name: "a", Version: "1.0.0", SHA256: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := NewPackageRepo(dir).SaveForWorkspace(ctx, "workspace-b", entity.Release{Name: "b", Version: "1.0.0", SHA256: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := NewDeviceRepo(dir).SaveForWorkspace(ctx, "workspace-a", entity.Device{ID: "a", Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := NewDeviceRepo(dir).SaveForWorkspace(ctx, "workspace-b", entity.Device{ID: "b", Name: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := NewAuditRepo(dir).AppendForWorkspace(ctx, "workspace-a", entity.AuditEvent{Action: "a", At: now}); err != nil {
		t.Fatal(err)
	}
	if err := NewAuditRepo(dir).AppendForWorkspace(ctx, "workspace-b", entity.AuditEvent{Action: "b", At: now}); err != nil {
		t.Fatal(err)
	}
	if err := workspaces.DeleteWorkspace(ctx, "workspace-a"); err != nil {
		t.Fatal(err)
	}
	if got, err := NewPackageRepo(dir).ListForWorkspace(ctx, "workspace-a"); err != nil || len(got) != 0 {
		t.Fatalf("packages remained: %+v %v", got, err)
	}
	if got, err := NewDeviceRepo(dir).ListForWorkspace(ctx, "workspace-a"); err != nil || len(got) != 0 {
		t.Fatalf("devices remained: %+v %v", got, err)
	}
	if got, err := NewAuditRepo(dir).ListForWorkspace(ctx, "workspace-a"); err != nil || len(got) != 0 {
		t.Fatalf("audit remained: %+v %v", got, err)
	}
	if got, err := NewPackageRepo(dir).ListForWorkspace(ctx, "workspace-b"); err != nil || len(got) != 1 {
		t.Fatalf("workspace-b packages changed: %+v %v", got, err)
	}
}
