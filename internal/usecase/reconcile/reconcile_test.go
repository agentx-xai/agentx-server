package reconcile

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo/file"
	"context"
	"testing"
)

func TestPlanIncludesInstallUpdateAndRemove(t *testing.T) {
	dir := t.TempDir()
	devices := file.NewDeviceRepo(dir)
	manifests := file.NewWorkspaceRepo(dir)
	ws := entity.Workspace{ID: "workspace-1", Slug: "one", Name: "One"}
	if err := manifests.Create(context.Background(), ws, entity.Membership{WorkspaceID: ws.ID, UserID: "token", Role: entity.RoleOwner}); err != nil {
		t.Fatal(err)
	}
	if err := devices.SaveForWorkspace(context.Background(), ws.ID, entity.Device{ID: "device-1", Name: "laptop", InstalledPackages: map[string]string{"old": "old-digest", "update": "old"}}); err != nil {
		t.Fatal(err)
	}
	if err := manifests.SaveManifest(context.Background(), entity.TeamManifest{ID: "m1", WorkspaceID: ws.ID, Revision: 1, Document: map[string]any{"version": 1, "packages": []any{map[string]any{"name": "new", "version": "1.0.0", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, map[string]any{"name": "update", "version": "2.0.0", "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}}}); err != nil {
		t.Fatal(err)
	}
	plan, err := New(devices, manifests).Plan(context.Background(), ws.ID, "device-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 3 {
		t.Fatalf("expected 3 actions, got %+v", plan.Actions)
	}
}
