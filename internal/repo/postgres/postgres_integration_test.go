package postgres

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentx/server/internal/entity"
	"github.com/google/uuid"
)

func TestPostgresRepositoriesAndMigration(t *testing.T) {
	url := os.Getenv("AGENTX_POSTGRES_TEST_URL")
	if url == "" {
		t.Skip("AGENTX_POSTGRES_TEST_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	migration, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "001_initial.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	userID := "integration|" + uuid.NewString()
	w := entity.Workspace{ID: uuid.NewString(), Slug: "integration-" + strings.ToLower(uuid.NewString()[:8]), Name: "Integration", CreatedAt: time.Now().UTC()}
	wr := WorkspaceRepository{Store: store}
	if err = wr.Create(ctx, w, entity.Membership{WorkspaceID: w.ID, UserID: userID, Role: entity.RoleOwner, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	workspaces, err := wr.List(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 1 || workspaces[0].ID != w.ID {
		t.Fatalf("workspace round trip failed: %+v", workspaces)
	}
	if err = wr.AddMember(ctx, entity.Membership{WorkspaceID: w.ID, UserID: "integration|member", Role: entity.RoleViewer, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	pr := PackageRepository{Store: store}
	release := entity.Release{Name: "integration", Version: "1.0.0", SHA256: "abc", Size: 3, Signature: "sig", SignatureStatus: "verified", CreatedAt: time.Now().UTC()}
	if err = pr.SaveForWorkspace(ctx, w.ID, release); err != nil {
		t.Fatal(err)
	}
	releases, err := pr.ListForWorkspace(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 1 || releases[0].SHA256 != "abc" || releases[0].SignatureStatus != "verified" {
		t.Fatalf("release round trip failed: %+v", releases)
	}
	if err = pr.SaveForWorkspace(ctx, w.ID, release); err == nil {
		t.Fatal("expected unique package/version constraint")
	}
	if err = pr.StoreIdempotency(ctx, w.ID, "request-1", release); err != nil {
		t.Fatal(err)
	}
	if got, found, err := pr.LookupIdempotency(ctx, w.ID, "request-1"); err != nil || !found || got.SHA256 != release.SHA256 {
		t.Fatalf("idempotency round trip failed: %+v %v %v", got, found, err)
	}
	dr := DeviceRepository{Store: store}
	device := entity.Device{ID: uuid.NewString(), Name: "integration-device", Agent: "Codex", Status: "online", UpdatedAt: time.Now().UTC(), InstalledPackages: map[string]string{"integration": "abc"}}
	if err = dr.SaveForWorkspace(ctx, w.ID, device); err != nil {
		t.Fatal(err)
	}
	devices, err := dr.ListForWorkspace(ctx, w.ID)
	if err != nil || len(devices) != 1 || devices[0].InstalledPackages["integration"] != "abc" {
		t.Fatalf("device package round trip failed: %+v %v", devices, err)
	}
	ar := AuditRepository{Store: store}
	if err = ar.AppendForWorkspace(ctx, w.ID, entity.AuditEvent{Action: "integration.check", ActorID: userID, ResourceType: "test", ResourceID: w.ID, Metadata: map[string]any{"check": true}, RequestID: "request-1", At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	events, err := ar.ListForWorkspace(ctx, w.ID)
	if err != nil || len(events) == 0 || events[0].ResourceType != "test" || events[0].ActorID != userID || events[0].RequestID != "request-1" || events[0].Metadata["check"] != true {
		t.Fatalf("audit round trip failed: %+v %v", events, err)
	}
	manifestRepo := WorkspaceRepository{Store: store}
	manifest := entity.TeamManifest{ID: uuid.NewString(), WorkspaceID: w.ID, Revision: 1, Document: map[string]any{"packages": []any{map[string]any{"name": "integration", "sha256": "abc"}}}, CreatedAt: time.Now().UTC()}
	if err = manifestRepo.SaveManifest(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	if got, err := manifestRepo.CurrentManifest(ctx, w.ID); err != nil || got.Revision != 1 {
		t.Fatalf("manifest round trip failed: %+v %v", got, err)
	}
	outbox := OutboxRepo{Store: store}
	if err = outbox.Enqueue(ctx, entity.OutboxEvent{ID: uuid.NewString(), Topic: "integration.check", Payload: map[string]any{"ok": true}, AvailableAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	eventsOutbox, err := outbox.Claim(ctx, 10)
	if err != nil || len(eventsOutbox) == 0 {
		t.Fatalf("outbox claim failed: %+v %v", eventsOutbox, err)
	}
	if err = outbox.MarkProcessed(ctx, eventsOutbox[0].ID); err != nil {
		t.Fatal(err)
	}
}
