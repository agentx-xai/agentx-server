package job_test

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/job"
	"agentx/server/internal/repo/file"
	"bytes"
	"context"
	"testing"
	"time"
)

func TestArtifactCleanupRetainsSharedDigestThenDeletesUnreferencedObject(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := file.NewArtifactStore(dir)
	packages := file.NewPackageRepo(dir)
	release, err := store.Put(ctx, "shared", bytes.NewBufferString("shared artifact"))
	if err != nil {
		t.Fatal(err)
	}
	release.Version = "1.0.0"
	release.CreatedAt = time.Now().UTC()
	if err = packages.SaveForWorkspace(ctx, "workspace-a", release); err != nil {
		t.Fatal(err)
	}
	if err = packages.SaveForWorkspace(ctx, "workspace-b", entity.Release{Name: "shared-copy", Version: "2.0.0", SHA256: release.SHA256, Size: release.Size, CreatedAt: release.CreatedAt}); err != nil {
		t.Fatal(err)
	}
	handler := job.ArtifactCleanupHandler{References: packages, Store: store}
	if err = packages.RemoveWorkspaceReleases(ctx, "workspace-a"); err != nil {
		t.Fatal(err)
	}
	if err = handler.Handle(ctx, map[string]any{"digest": release.SHA256}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Stat(ctx, release.SHA256); err != nil {
		t.Fatalf("shared artifact was deleted: %v", err)
	}
	if err = packages.RemoveWorkspaceReleases(ctx, "workspace-b"); err != nil {
		t.Fatal(err)
	}
	if err = handler.Handle(ctx, map[string]any{"digest": release.SHA256}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Stat(ctx, release.SHA256); err == nil {
		t.Fatal("unreferenced artifact was retained")
	}
	if err = handler.Handle(ctx, map[string]any{"digest": "not-a-digest"}); err == nil {
		t.Fatal("invalid cleanup payload was accepted")
	}
}
