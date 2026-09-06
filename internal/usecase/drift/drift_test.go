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
