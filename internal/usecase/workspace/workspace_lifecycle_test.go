package workspace_test

import (
	"agentx/server/internal/auth"
	"agentx/server/internal/job"
	"agentx/server/internal/repo/file"
	workspaceusecase "agentx/server/internal/usecase/workspace"
	"bytes"
	"context"
	"testing"
	"time"
)

func TestWorkspaceDeletionGarbageCollectsOnlyLastArtifactReference(t *testing.T) {
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{UserID: "issuer|owner"})
	dir := t.TempDir()
	workspaceRepo := file.NewWorkspaceRepo(dir)
	packages := file.NewPackageRepo(dir)
	store := file.NewArtifactStore(dir)
	audit := file.NewAuditRepo(dir)
	outbox := file.NewOutboxRepo(dir)
	service := workspaceusecase.New(workspaceRepo)
	first, err := service.Create(ctx, "First", "first-workspace")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(ctx, "Second", "second-workspace")
	if err != nil {
		t.Fatal(err)
	}
	release, err := store.Put(ctx, "shared", bytes.NewBufferString("shared lifecycle artifact"))
	if err != nil {
		t.Fatal(err)
	}
	release.Version = "1.0.0"
	release.CreatedAt = time.Now().UTC()
	if err = packages.SaveForWorkspace(ctx, first.ID, release); err != nil {
		t.Fatal(err)
	}
	copy := release
	copy.Name = "shared-copy"
	if err = packages.SaveForWorkspace(ctx, second.ID, copy); err != nil {
		t.Fatal(err)
	}
	service.SetArtifactLifecycle(packages)
	service.SetAudit(audit)
	service.SetOutbox(outbox)
	cleanup := job.ArtifactCleanupHandler{References: packages, Store: store}
	worker := job.Worker{Outbox: outbox, Handler: func(handlerCtx context.Context, topic string, payload map[string]any) error {
		if topic == "artifact.cleanup" {
			return cleanup.Handle(handlerCtx, payload)
		}
		return nil
	}}

	if err = service.Delete(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if err = worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Stat(ctx, release.SHA256); err != nil {
		t.Fatalf("artifact shared by second workspace was deleted: %v", err)
	}
	if releases, listErr := packages.ListForWorkspace(ctx, first.ID); listErr != nil || len(releases) != 0 {
		t.Fatalf("deleted workspace release metadata remains: %+v %v", releases, listErr)
	}
	events, err := audit.ListForWorkspace(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "workspace.delete" || events[0].Metadata["artifact_candidates"] != float64(1) {
		t.Fatalf("workspace deletion audit missing: %+v", events)
	}

	if err = service.Delete(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if err = worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Stat(ctx, release.SHA256); err == nil {
		t.Fatal("artifact remained after its last workspace reference was deleted")
	}
}
