package postgres

import (
	"agentx/server/internal/auth"
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	workspaceusecase "agentx/server/internal/usecase/workspace"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type failingOutbox struct{}

func (failingOutbox) Enqueue(context.Context, entity.OutboxEvent) error {
	return errors.New("forced outbox failure")
}
func (failingOutbox) Claim(context.Context, int) ([]entity.OutboxEvent, error) { return nil, nil }
func (failingOutbox) MarkProcessed(context.Context, string) error              { return nil }
func (failingOutbox) MarkFailed(context.Context, string, time.Time) error      { return nil }
func (failingOutbox) MarkDeadLetter(context.Context, string, string) error     { return nil }

func applyTestMigrations(t *testing.T, store *Store, ctx context.Context) {
	t.Helper()
	for _, migrationName := range []string{"001_initial.sql"} {
		migration, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", migrationName))
		if err != nil {
			t.Fatal(err)
		}
		up := strings.SplitN(string(migration), "-- +goose Down", 2)[0]
		if _, err = store.Pool.Exec(ctx, up); err != nil {
			t.Fatal(err)
		}
	}
	var hasWorkspaceID bool
	if err := store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='device_packages' AND column_name='workspace_id')`).Scan(&hasWorkspaceID); err != nil {
		t.Fatal(err)
	}
	if !hasWorkspaceID {
		migration, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "002_workspace_scoped_device_ids.sql"))
		if err != nil {
			t.Fatal(err)
		}
		up := strings.SplitN(string(migration), "-- +goose Down", 2)[0]
		if _, err = store.Pool.Exec(ctx, up); err != nil {
			t.Fatal(err)
		}
	}
	var hasRateLimits bool
	if err := store.Pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.rate_limit_windows') IS NOT NULL`).Scan(&hasRateLimits); err != nil {
		t.Fatal(err)
	}
	if !hasRateLimits {
		migration, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "003_shared_rate_limits.sql"))
		if err != nil {
			t.Fatal(err)
		}
		up := strings.SplitN(string(migration), "-- +goose Down", 2)[0]
		if _, err = store.Pool.Exec(ctx, up); err != nil {
			t.Fatal(err)
		}
	}
	var hasIdempotencyResourceType bool
	if err := store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='idempotency_keys' AND column_name='resource_type')`).Scan(&hasIdempotencyResourceType); err != nil {
		t.Fatal(err)
	}
	if !hasIdempotencyResourceType {
		migration, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "004_idempotency_resource_types.sql"))
		if err != nil {
			t.Fatal(err)
		}
		up := strings.SplitN(string(migration), "-- +goose Down", 2)[0]
		if _, err = store.Pool.Exec(ctx, up); err != nil {
			t.Fatal(err)
		}
	}
	var hasInvitations bool
	if err := store.Pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.workspace_invitations') IS NOT NULL`).Scan(&hasInvitations); err != nil {
		t.Fatal(err)
	}
	if !hasInvitations {
		migration, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "005_workspace_invitations.sql"))
		if err != nil {
			t.Fatal(err)
		}
		up := strings.SplitN(string(migration), "-- +goose Down", 2)[0]
		if _, err = store.Pool.Exec(ctx, up); err != nil {
			t.Fatal(err)
		}
	}
	var auditDeleteRule string
	err := store.Pool.QueryRow(ctx, `SELECT delete_rule FROM information_schema.referential_constraints WHERE constraint_schema=current_schema() AND constraint_name='audit_events_workspace_id_fkey'`).Scan(&auditDeleteRule)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
	if auditDeleteRule != "SET NULL" {
		migration, readErr := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "006_workspace_deletion_audit.sql"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		up := strings.SplitN(string(migration), "-- +goose Down", 2)[0]
		if _, err = store.Pool.Exec(ctx, up); err != nil {
			t.Fatal(err)
		}
	}
	var invitationActorDeleteRule string
	err = store.Pool.QueryRow(ctx, `SELECT delete_rule FROM information_schema.referential_constraints WHERE constraint_schema=current_schema() AND constraint_name='workspace_invitations_created_by_fkey'`).Scan(&invitationActorDeleteRule)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
	if invitationActorDeleteRule != "SET NULL" {
		migration, readErr := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "007_account_deletion.sql"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		up := strings.SplitN(string(migration), "-- +goose Down", 2)[0]
		if _, err = store.Pool.Exec(ctx, up); err != nil {
			t.Fatal(err)
		}
	}
	var hasLegalHolds bool
	if err = store.Pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.legal_holds') IS NOT NULL`).Scan(&hasLegalHolds); err != nil {
		t.Fatal(err)
	}
	if !hasLegalHolds {
		migration, readErr := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "008_legal_holds.sql"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		up := strings.SplitN(string(migration), "-- +goose Down", 2)[0]
		if _, err = store.Pool.Exec(ctx, up); err != nil {
			t.Fatal(err)
		}
	}
}

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
	applyTestMigrations(t, store, ctx)
	userID := "integration|" + uuid.NewString()
	w := entity.Workspace{ID: uuid.NewString(), Slug: "integration-" + strings.ToLower(uuid.NewString()[:8]), Name: "Integration", CreatedAt: time.Now().UTC()}
	wr := WorkspaceRepository{Store: store}
	if err = wr.Create(ctx, w, entity.Membership{WorkspaceID: w.ID, UserID: userID, Role: entity.RoleOwner, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	duplicateWorkspace := entity.Workspace{ID: uuid.NewString(), Slug: w.Slug, Name: "Duplicate", CreatedAt: time.Now().UTC()}
	if err = wr.Create(ctx, duplicateWorkspace, entity.Membership{WorkspaceID: duplicateWorkspace.ID, UserID: userID, Role: entity.RoleOwner, CreatedAt: time.Now().UTC()}); !errors.Is(err, repo.ErrConflict) {
		t.Fatalf("duplicate workspace slug must be a conflict, got %v", err)
	}
	if _, err = wr.Membership(ctx, w.ID, "integration|missing"); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("missing membership must be not found, got %v", err)
	}
	workspaces, err := wr.List(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 1 || workspaces[0].ID != w.ID {
		t.Fatalf("workspace round trip failed: %+v", workspaces)
	}
	if other, err := wr.List(ctx, "integration|other"); err != nil || len(other) != 0 {
		t.Fatalf("workspace leaked to another user: %+v %v", other, err)
	}
	if err = wr.AddMember(ctx, entity.Membership{WorkspaceID: w.ID, UserID: "integration|member", Role: entity.RoleViewer, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	invitation := entity.WorkspaceInvitation{ID: uuid.NewString(), WorkspaceID: w.ID, Email: "integration-" + strings.ToLower(uuid.NewString()[:8]) + "@example.com", Role: entity.RoleDeveloper, CreatedBy: userID, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if err = wr.CreateInvitation(ctx, invitation); err != nil {
		t.Fatal(err)
	}
	if err = wr.CreateInvitation(ctx, entity.WorkspaceInvitation{ID: uuid.NewString(), WorkspaceID: w.ID, Email: invitation.Email, Role: entity.RoleViewer, CreatedBy: userID, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour)}); !errors.Is(err, repo.ErrConflict) {
		t.Fatalf("duplicate pending invitation must conflict, got %v", err)
	}
	invitationPage, err := wr.InvitationsForWorkspacePage(ctx, w.ID, repo.PageRequest{Limit: 1})
	if err != nil || invitationPage.Total != 1 || len(invitationPage.Items) != 1 || invitationPage.Items[0].Status != "pending" {
		t.Fatalf("invitation page failed: %+v %v", invitationPage, err)
	}
	invitee := entity.User{ID: "integration-invitee|" + uuid.NewString(), Issuer: "integration-invitee", Subject: uuid.NewString(), Email: invitation.Email, EmailVerified: true, CreatedAt: time.Now().UTC()}
	claimed, err := wr.ClaimInvitation(ctx, invitation, invitee, time.Now().UTC())
	if err != nil || claimed.Status != "accepted" || claimed.AcceptedBy != invitee.ID {
		t.Fatalf("invitation claim failed: %+v %v", claimed, err)
	}
	claimedMembership, err := wr.Membership(ctx, w.ID, invitee.ID)
	if err != nil || claimedMembership.Role != entity.RoleDeveloper {
		t.Fatalf("claimed membership failed: %+v %v", claimedMembership, err)
	}
	var storedEmail string
	var storedVerified bool
	if err = store.Pool.QueryRow(ctx, `SELECT email,email_verified FROM users WHERE id=$1`, invitee.ID).Scan(&storedEmail, &storedVerified); err != nil || storedEmail != invitation.Email || !storedVerified {
		t.Fatalf("verified user was not persisted: email=%q verified=%v err=%v", storedEmail, storedVerified, err)
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
	if err = pr.SaveForWorkspace(ctx, w.ID, release); !errors.Is(err, repo.ErrConflict) {
		t.Fatalf("duplicate release must be a conflict, got %v", err)
	}
	if _, err = pr.FindReleaseByDigestForWorkspace(ctx, w.ID, "missing"); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("missing artifact must be not found, got %v", err)
	}
	if _, err = pr.FindReleaseForWorkspace(ctx, w.ID, release.Name, "2.0.0"); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("missing release must be not found, got %v", err)
	}
	if _, err = pr.ApproveReleaseForWorkspace(ctx, w.ID, release.Name, "2.0.0"); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("missing approval target must be not found, got %v", err)
	}
	record := entity.IdempotencyRecord{Fingerprint: "fingerprint-1", Release: release}
	if err = pr.StoreIdempotency(ctx, w.ID, "request-1", record); err != nil {
		t.Fatal(err)
	}
	if got, found, err := pr.LookupIdempotency(ctx, w.ID, "request-1"); err != nil || !found || got.Release.SHA256 != release.SHA256 || got.Fingerprint != record.Fingerprint {
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
	otherWorkspace := entity.Workspace{ID: uuid.NewString(), Slug: "integration-other-" + strings.ToLower(uuid.NewString()[:8]), Name: "Integration Other", CreatedAt: time.Now().UTC()}
	if err = wr.Create(ctx, otherWorkspace, entity.Membership{WorkspaceID: otherWorkspace.ID, UserID: userID, Role: entity.RoleOwner, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	sharedID := "laptop-2"
	if err = dr.SaveForWorkspace(ctx, w.ID, entity.Device{ID: sharedID, Name: "Primary laptop", UpdatedAt: time.Now().UTC(), InstalledPackages: map[string]string{"shared": "workspace-a"}}); err != nil {
		t.Fatal(err)
	}
	if err = dr.SaveForWorkspace(ctx, otherWorkspace.ID, entity.Device{ID: sharedID, Name: "Other laptop", UpdatedAt: time.Now().UTC(), InstalledPackages: map[string]string{"shared": "workspace-b"}}); err != nil {
		t.Fatal(err)
	}
	otherDevices, err := dr.ListForWorkspace(ctx, otherWorkspace.ID)
	if err != nil || len(otherDevices) != 1 || otherDevices[0].InstalledPackages["shared"] != "workspace-b" {
		t.Fatalf("workspace-scoped device IDs leaked package state: %+v %v", otherDevices, err)
	}
	ar := AuditRepository{Store: store}
	if err = ar.AppendForWorkspace(ctx, w.ID, entity.AuditEvent{Action: "integration.check", ActorID: userID, ResourceType: "test", ResourceID: w.ID, Metadata: map[string]any{"check": true}, RequestID: "request-1", At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	events, err := ar.ListForWorkspace(ctx, w.ID)
	if err != nil || len(events) == 0 || events[0].ResourceType != "test" || events[0].ActorID != userID || events[0].RequestID != "request-1" || events[0].Metadata["check"] != true {
		t.Fatalf("audit round trip failed: %+v %v", events, err)
	}
	pageRequest := repo.PageRequest{Limit: 1, Offset: 1}
	workspacePage, err := wr.ListPage(ctx, userID, pageRequest)
	if err != nil || workspacePage.Total != 2 || len(workspacePage.Items) != 1 {
		t.Fatalf("workspace SQL page failed: %+v %v", workspacePage, err)
	}
	membershipPage, err := wr.MembersPage(ctx, w.ID, pageRequest)
	if err != nil || membershipPage.Total != 3 || len(membershipPage.Items) != 1 {
		t.Fatalf("membership SQL page failed: %+v %v", membershipPage, err)
	}
	releasePage, err := pr.ListPageForWorkspace(ctx, w.ID, repo.PageRequest{Limit: 1})
	if err != nil || releasePage.Total != 1 || len(releasePage.Items) != 1 {
		t.Fatalf("release SQL page failed: %+v %v", releasePage, err)
	}
	if _, err = pr.ApproveReleaseForWorkspace(ctx, w.ID, release.Name, release.Version); !errors.Is(err, repo.ErrConflict) {
		t.Fatalf("published release approval must be a conflict, got %v", err)
	}
	pending := entity.Release{Name: release.Name, Version: "1.1.0", SHA256: "def", Status: "pending_approval", CreatedAt: time.Now().UTC()}
	if err = pr.SaveForWorkspace(ctx, w.ID, pending); err != nil {
		t.Fatal(err)
	}
	if _, err = pr.ApproveReleaseForWorkspace(ctx, w.ID, pending.Name, pending.Version); err != nil {
		t.Fatalf("pending release approval failed: %v", err)
	}
	if _, err = pr.ApproveReleaseForWorkspace(ctx, w.ID, pending.Name, pending.Version); !errors.Is(err, repo.ErrConflict) {
		t.Fatalf("repeated approval must be a conflict, got %v", err)
	}
	devicePage, err := dr.ListPageForWorkspace(ctx, w.ID, pageRequest)
	if err != nil || devicePage.Total != 2 || len(devicePage.Items) != 1 {
		t.Fatalf("device SQL page failed: %+v %v", devicePage, err)
	}
	auditPage, err := ar.ListPageForWorkspace(ctx, w.ID, repo.PageRequest{Limit: 1})
	if err != nil || auditPage.Total != 1 || len(auditPage.Items) != 1 {
		t.Fatalf("audit SQL page failed: %+v %v", auditPage, err)
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
	outboxID := uuid.NewString()
	if err = outbox.Enqueue(ctx, entity.OutboxEvent{ID: outboxID, Topic: "integration.check", Payload: map[string]any{"ok": true}, AvailableAt: time.Now().UTC().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	var storedOutbox int
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE id=$1 AND topic='integration.check'`, outboxID).Scan(&storedOutbox); err != nil || storedOutbox != 1 {
		t.Fatalf("outbox round trip failed: count=%d %v", storedOutbox, err)
	}
	if err = outbox.MarkProcessed(ctx, outboxID); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresAccountExportDeletionAndRollback(t *testing.T) {
	url := os.Getenv("AGENTX_POSTGRES_TEST_URL")
	if url == "" {
		t.Skip("AGENTX_POSTGRES_TEST_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	applyTestMigrations(t, store, ctx)
	workspaceRepo := WorkspaceRepository{Store: store}
	accountRepo := AccountRepository{Store: store}
	auditRepo := AuditRepository{Store: store}
	ownerID := "account-owner|" + uuid.NewString()
	memberID := "account-member|" + uuid.NewString()
	memberEmail := "account-" + strings.ToLower(uuid.NewString()[:8]) + "@example.com"
	workspace := entity.Workspace{ID: uuid.NewString(), Slug: "account-" + strings.ToLower(uuid.NewString()[:8]), Name: "Account lifecycle", CreatedAt: time.Now().UTC()}
	if err = workspaceRepo.Create(ctx, workspace, entity.Membership{WorkspaceID: workspace.ID, UserID: ownerID, Role: entity.RoleOwner, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	defer workspaceRepo.DeleteWorkspace(context.Background(), workspace.ID)
	if err = workspaceRepo.AddMember(ctx, entity.Membership{WorkspaceID: workspace.ID, UserID: memberID, Role: entity.RoleViewer, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Pool.Exec(ctx, `UPDATE users SET email=$2,email_verified=true WHERE id=$1`, memberID, memberEmail); err != nil {
		t.Fatal(err)
	}
	invitation := entity.WorkspaceInvitation{ID: uuid.NewString(), WorkspaceID: workspace.ID, Email: memberEmail, Role: entity.RoleViewer, CreatedBy: ownerID, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if err = workspaceRepo.CreateInvitation(ctx, invitation); err != nil {
		t.Fatal(err)
	}
	if err = auditRepo.AppendForWorkspace(ctx, workspace.ID, entity.AuditEvent{ActorID: memberID, Action: "account.integration", ResourceType: "test", ResourceID: memberID, Metadata: map[string]any{"private": "not exported"}, RequestID: "account-export-request", At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Pool.Exec(ctx, `INSERT INTO outbox(id,topic,payload_json) VALUES($1,'membership.updated',jsonb_build_object('workspace_id',$2::text,'user_id',$3::text))`, uuid.NewString(), workspace.ID, memberID); err != nil {
		t.Fatal(err)
	}

	export, err := accountRepo.ExportAccount(ctx, memberID, memberEmail)
	if err != nil {
		t.Fatal(err)
	}
	if export.User.Email != memberEmail || len(export.Memberships) != 1 || export.Memberships[0].Workspace.ID != workspace.ID || len(export.Invitations) != 1 || len(export.AuditEvents) != 1 || export.AuditEvents[0].WorkspaceID != workspace.ID {
		t.Fatalf("incomplete account export: %+v", export)
	}

	triggerName := "fail_account_delete_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	functionName := triggerName + "_fn"
	if _, err = store.Pool.Exec(ctx, `CREATE FUNCTION `+functionName+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.topic='account.deleted' THEN RAISE EXCEPTION 'forced account deletion failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER `+triggerName+` BEFORE INSERT ON outbox FOR EACH ROW EXECUTE FUNCTION `+functionName+`()`); err != nil {
		t.Fatal(err)
	}
	defer store.Pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS `+triggerName+` ON outbox; DROP FUNCTION IF EXISTS `+functionName+`()`)
	deletion := entity.AccountDeletion{UserID: memberID, Email: memberEmail, DeletionID: uuid.NewString(), RequestID: "account-delete-request", At: time.Now().UTC()}
	if err = accountRepo.DeleteAccount(ctx, deletion); err == nil || !strings.Contains(err.Error(), "forced account deletion failure") {
		t.Fatalf("expected transactional failure, got %v", err)
	}
	if _, err = workspaceRepo.Membership(ctx, workspace.ID, memberID); err != nil {
		t.Fatalf("failed deletion removed membership: %v", err)
	}
	var actorID string
	if err = store.Pool.QueryRow(ctx, `SELECT actor_id FROM audit_events WHERE action='account.integration' AND actor_id=$1`, memberID).Scan(&actorID); err != nil || actorID != memberID {
		t.Fatalf("failed deletion anonymized audit: %q %v", actorID, err)
	}
	if _, err = store.Pool.Exec(ctx, `DROP TRIGGER `+triggerName+` ON outbox; DROP FUNCTION `+functionName+`()`); err != nil {
		t.Fatal(err)
	}

	if err = accountRepo.DeleteAccount(ctx, deletion); err != nil {
		t.Fatal(err)
	}
	if _, err = workspaceRepo.Membership(ctx, workspace.ID, memberID); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("deleted membership remains: %v", err)
	}
	var userCount, invitationCount, actorCount, payloadCount, deletionAuditCount, deletionOutboxCount int
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id=$1`, memberID).Scan(&userCount); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM workspace_invitations WHERE email=$1`, memberEmail).Scan(&invitationCount); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_id=$1`, memberID).Scan(&actorCount); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE payload_json->>'user_id'=$1`, memberID).Scan(&payloadCount); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='account.delete' AND resource_id=$1 AND actor_id IS NULL`, deletion.DeletionID).Scan(&deletionAuditCount); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE id=$1 AND topic='account.deleted' AND NOT payload_json ? 'user_id'`, deletion.DeletionID).Scan(&deletionOutboxCount); err != nil {
		t.Fatal(err)
	}
	if userCount != 0 || invitationCount != 0 || actorCount != 0 || payloadCount != 0 || deletionAuditCount != 1 || deletionOutboxCount != 1 {
		t.Fatalf("account deletion incomplete: users=%d invitations=%d actors=%d payloads=%d audit=%d outbox=%d", userCount, invitationCount, actorCount, payloadCount, deletionAuditCount, deletionOutboxCount)
	}

	ownerDeletion := entity.AccountDeletion{UserID: ownerID, DeletionID: uuid.NewString(), At: time.Now().UTC()}
	if err = accountRepo.DeleteAccount(ctx, ownerDeletion); !errors.Is(err, repo.ErrConflict) {
		t.Fatalf("owner account deletion must fail: %v", err)
	}
	if _, err = workspaceRepo.Membership(ctx, workspace.ID, ownerID); err != nil {
		t.Fatalf("owner conflict mutated membership: %v", err)
	}
}

func TestPostgresRateLimitIsSharedAcrossInstances(t *testing.T) {
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
	applyTestMigrations(t, store, ctx)
	first := NewRateLimiter(store, 2)
	second := NewRateLimiter(store, 2)
	identity := "rate-limit-test-" + uuid.NewString()
	for index, limiter := range []*RateLimiter{first, second, first} {
		allowed, retryAfter, err := limiter.Allow(ctx, identity)
		if err != nil {
			t.Fatal(err)
		}
		if allowed != (index < 2) {
			t.Fatalf("request %d allowed=%v", index+1, allowed)
		}
		if retryAfter <= 0 || retryAfter > time.Minute {
			t.Fatalf("invalid retry interval: %s", retryAfter)
		}
	}
}

func TestPostgresOutboxClaimLeasesAcrossWorkers(t *testing.T) {
	databaseURL := os.Getenv("AGENTX_POSTGRES_TEST_URL")
	if databaseURL == "" {
		t.Skip("AGENTX_POSTGRES_TEST_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	adminStore, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer adminStore.Close()
	schemaName := "outbox_lease_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = adminStore.Pool.Exec(ctx, `CREATE SCHEMA `+schemaName); err != nil {
		t.Fatal(err)
	}
	defer adminStore.Pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+schemaName+` CASCADE`)
	parsedURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsedURL.Query()
	query.Set("search_path", schemaName)
	parsedURL.RawQuery = query.Encode()
	isolatedURL := parsedURL.String()

	firstStore, err := Open(ctx, isolatedURL)
	if err != nil {
		t.Fatal(err)
	}
	defer firstStore.Close()
	secondStore, err := Open(ctx, isolatedURL)
	if err != nil {
		t.Fatal(err)
	}
	defer secondStore.Close()
	applyTestMigrations(t, firstStore, ctx)
	eventID := uuid.NewString()
	first := OutboxRepo{Store: firstStore}
	second := OutboxRepo{Store: secondStore}
	if err = first.Enqueue(ctx, entity.OutboxEvent{ID: eventID, Topic: "integration.lease", Payload: map[string]any{"ok": true}, AvailableAt: time.Now().UTC().Add(-24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan []entity.OutboxEvent, 2)
	errors := make(chan error, 2)
	var workers sync.WaitGroup
	for _, outbox := range []OutboxRepo{first, second} {
		workers.Add(1)
		go func(candidate OutboxRepo) {
			defer workers.Done()
			<-start
			items, claimErr := candidate.Claim(ctx, 1)
			results <- items
			errors <- claimErr
		}(outbox)
	}
	close(start)
	workers.Wait()
	close(results)
	close(errors)
	for claimErr := range errors {
		if claimErr != nil {
			t.Fatal(claimErr)
		}
	}
	claimed := 0
	for items := range results {
		claimed += len(items)
		if len(items) == 1 && (items[0].ID != eventID || items[0].Attempts != 1) {
			t.Fatalf("unexpected claimed event: %+v", items[0])
		}
	}
	if claimed != 1 {
		t.Fatalf("expected exactly one worker to claim the event, got %d", claimed)
	}
	if err = first.MarkProcessed(ctx, eventID); err != nil {
		t.Fatal(err)
	}
	if err = first.MarkProcessed(ctx, uuid.NewString()); err == nil {
		t.Fatal("expected missing acknowledgement to fail")
	}
}

func TestPostgresIdempotentReleaseIsAtomic(t *testing.T) {
	url := os.Getenv("AGENTX_POSTGRES_TEST_URL")
	if url == "" {
		t.Skip("AGENTX_POSTGRES_TEST_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	applyTestMigrations(t, store, ctx)
	workspaceID := uuid.NewString()
	workspaceRepo := WorkspaceRepository{Store: store}
	if err = workspaceRepo.Create(ctx,
		entity.Workspace{ID: workspaceID, Slug: "atomic-" + strings.ToLower(uuid.NewString()[:8]), Name: "Atomic", CreatedAt: time.Now().UTC()},
		entity.Membership{WorkspaceID: workspaceID, UserID: "atomic|owner", Role: entity.RoleOwner, CreatedAt: time.Now().UTC()},
	); err != nil {
		t.Fatal(err)
	}
	repository := PackageRepository{Store: store}
	record := entity.IdempotencyRecord{Fingerprint: "fingerprint", Release: entity.Release{Name: "atomic-release", Version: "1.0.0", SHA256: strings.Repeat("a", 64), Size: 1, CreatedAt: time.Now().UTC()}}
	audit := entity.AuditEvent{Action: "release.publish", ActorID: "atomic|owner", ResourceType: "release", ResourceID: "atomic-release@1.0.0", RequestID: "atomic-request", At: time.Now().UTC()}
	outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "release.published", Payload: map[string]any{"workspace_id": workspaceID}, AvailableAt: time.Now().UTC()}
	type result struct {
		replayed bool
		err      error
	}
	results := make(chan result, 8)
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, replayed, callErr := repository.SaveForWorkspaceIdempotent(ctx, workspaceID, "atomic-key", record, audit, outbox)
			results <- result{replayed: replayed, err: callErr}
		}()
	}
	group.Wait()
	close(results)
	created, replayed := 0, 0
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.replayed {
			replayed++
		} else {
			created++
		}
	}
	if created != 1 || replayed != 7 {
		t.Fatalf("expected one create and seven replays, got created=%d replayed=%d", created, replayed)
	}
	mismatch := record
	mismatch.Fingerprint = "different"
	if _, _, err = repository.SaveForWorkspaceIdempotent(ctx, workspaceID, "atomic-key", mismatch, audit, outbox); !errors.Is(err, repo.ErrIdempotencyConflict) {
		t.Fatalf("expected fingerprint mismatch, got %v", err)
	}
	var releaseCount int
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM releases r JOIN packages p ON p.id=r.package_id WHERE p.workspace_id=$1 AND p.name=$2`, workspaceID, record.Release.Name).Scan(&releaseCount); err != nil {
		t.Fatal(err)
	}
	if releaseCount != 1 {
		t.Fatalf("expected one release row, got %d", releaseCount)
	}
	var auditCount, outboxCount int
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE workspace_id=$1 AND request_id=$2`, workspaceID, audit.RequestID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE id=$1`, outbox.ID).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 || outboxCount != 1 {
		t.Fatalf("release transaction lost or duplicated events: audit=%d outbox=%d", auditCount, outboxCount)
	}
}

func TestPostgresIdempotentDeviceIsAtomic(t *testing.T) {
	url := os.Getenv("AGENTX_POSTGRES_TEST_URL")
	if url == "" {
		t.Skip("AGENTX_POSTGRES_TEST_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	applyTestMigrations(t, store, ctx)
	workspaceID := uuid.NewString()
	workspaceRepo := WorkspaceRepository{Store: store}
	if err = workspaceRepo.Create(ctx,
		entity.Workspace{ID: workspaceID, Slug: "device-idem-" + strings.ToLower(uuid.NewString()[:8]), Name: "Device idempotency", CreatedAt: time.Now().UTC()},
		entity.Membership{WorkspaceID: workspaceID, UserID: "device-idem|owner", Role: entity.RoleOwner, CreatedAt: time.Now().UTC()},
	); err != nil {
		t.Fatal(err)
	}
	repository := DeviceRepository{Store: store}
	device := entity.Device{ID: uuid.NewString(), Name: "idempotent-device", Agent: "agentx", Status: "online", UpdatedAt: time.Now().UTC()}
	record := entity.DeviceIdempotencyRecord{Fingerprint: "device-fingerprint", Device: device}
	audit := entity.AuditEvent{Action: "device.register", ActorID: "device-idem|owner", ResourceType: "device", ResourceID: device.ID, RequestID: "device-idem-request", At: device.UpdatedAt}
	outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "device.registered", Payload: map[string]any{"workspace_id": workspaceID, "device_id": device.ID}, AvailableAt: device.UpdatedAt}
	type result struct {
		device   entity.Device
		replayed bool
		err      error
	}
	results := make(chan result, 8)
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			saved, replayed, callErr := repository.SaveForWorkspaceIdempotent(ctx, workspaceID, "device-key", record, audit, outbox)
			results <- result{device: saved, replayed: replayed, err: callErr}
		}()
	}
	group.Wait()
	close(results)
	created, replayed := 0, 0
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.device.ID != device.ID {
			t.Fatalf("unexpected replayed device: %+v", result.device)
		}
		if result.replayed {
			replayed++
		} else {
			created++
		}
	}
	if created != 1 || replayed != 7 {
		t.Fatalf("expected one create and seven replays, got created=%d replayed=%d", created, replayed)
	}
	mismatch := record
	mismatch.Fingerprint = "different"
	if _, _, err = repository.SaveForWorkspaceIdempotent(ctx, workspaceID, "device-key", mismatch, audit, outbox); !errors.Is(err, repo.ErrIdempotencyConflict) {
		t.Fatalf("expected fingerprint mismatch, got %v", err)
	}
	var deviceCount, auditCount, outboxCount, keyCount int
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM devices WHERE workspace_id=$1 AND id=$2`, workspaceID, device.ID).Scan(&deviceCount); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE workspace_id=$1 AND request_id=$2`, workspaceID, audit.RequestID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE id=$1`, outbox.ID).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_keys WHERE workspace_id=$1 AND key='device-key' AND resource_type='device'`, workspaceID).Scan(&keyCount); err != nil {
		t.Fatal(err)
	}
	if deviceCount != 1 || auditCount != 1 || outboxCount != 1 || keyCount != 1 {
		t.Fatalf("device transaction lost or duplicated rows: device=%d audit=%d outbox=%d key=%d", deviceCount, auditCount, outboxCount, keyCount)
	}
}

func TestWorkspaceMutationRollsBackAndCommitsEventsAtomically(t *testing.T) {
	url := os.Getenv("AGENTX_POSTGRES_TEST_URL")
	if url == "" {
		t.Skip("AGENTX_POSTGRES_TEST_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	applyTestMigrations(t, store, ctx)
	workspaceRepo := WorkspaceRepository{Store: store}
	auditRepo := AuditRepository{Store: store}
	principal := "atomic-workspace|" + uuid.NewString()
	requestID := "atomic-workspace-" + uuid.NewString()
	requestCtx := auth.WithRequestID(auth.WithPrincipal(ctx, auth.Principal{UserID: principal}), requestID)

	service := workspaceusecase.New(workspaceRepo)
	service.SetAudit(auditRepo)
	service.SetOutbox(failingOutbox{})
	service.SetUnitOfWork(store)
	failedSlug := "atomic-failed-" + strings.ToLower(uuid.NewString()[:8])
	if _, err = service.Create(requestCtx, "Atomic failed", failedSlug); err == nil || !strings.Contains(err.Error(), "forced outbox failure") {
		t.Fatalf("expected forced transaction failure, got %v", err)
	}
	var failedWorkspaces, failedAudits int
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE slug=$1`, failedSlug).Scan(&failedWorkspaces); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE request_id=$1`, requestID).Scan(&failedAudits); err != nil {
		t.Fatal(err)
	}
	if failedWorkspaces != 0 || failedAudits != 0 {
		t.Fatalf("failed mutation left partial rows: workspaces=%d audit=%d", failedWorkspaces, failedAudits)
	}

	service.SetOutbox(OutboxRepo{Store: store})
	successSlug := "atomic-success-" + strings.ToLower(uuid.NewString()[:8])
	created, err := service.Create(requestCtx, "Atomic success", successSlug)
	if err != nil {
		t.Fatal(err)
	}
	var workspaceXID, auditXID, outboxXID string
	if err = store.Pool.QueryRow(ctx, `SELECT xmin::text FROM workspaces WHERE id=$1`, created.ID).Scan(&workspaceXID); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT xmin::text FROM audit_events WHERE workspace_id=$1 AND action='workspace.create'`, created.ID).Scan(&auditXID); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT xmin::text FROM outbox WHERE payload_json->>'workspace_id'=$1 AND topic='workspace.created'`, created.ID).Scan(&outboxXID); err != nil {
		t.Fatal(err)
	}
	if workspaceXID != auditXID || workspaceXID != outboxXID {
		t.Fatalf("mutation rows used different transactions: workspace=%s audit=%s outbox=%s", workspaceXID, auditXID, outboxXID)
	}

	service.SetOutbox(failingOutbox{})
	failedInvitationEmail := "failed-" + strings.ToLower(uuid.NewString()[:8]) + "@example.com"
	if _, err = service.CreateInvitation(requestCtx, created.ID, failedInvitationEmail, entity.RoleViewer, time.Hour); err == nil || !strings.Contains(err.Error(), "forced outbox failure") {
		t.Fatalf("expected invitation transaction failure, got %v", err)
	}
	var failedInvitations int
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM workspace_invitations WHERE workspace_id=$1 AND email=$2`, created.ID, failedInvitationEmail).Scan(&failedInvitations); err != nil {
		t.Fatal(err)
	}
	if failedInvitations != 0 {
		t.Fatalf("failed invitation mutation left %d rows", failedInvitations)
	}

	service.SetOutbox(OutboxRepo{Store: store})
	invitationRequestID := "atomic-invitation-" + uuid.NewString()
	invitationCtx := auth.WithRequestID(auth.WithPrincipal(ctx, auth.Principal{UserID: principal}), invitationRequestID)
	invitation, err := service.CreateInvitation(invitationCtx, created.ID, "claim-"+strings.ToLower(uuid.NewString()[:8])+"@example.com", entity.RoleDeveloper, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var invitationXID, invitationAuditXID, invitationOutboxXID string
	if err = store.Pool.QueryRow(ctx, `SELECT xmin::text FROM workspace_invitations WHERE id=$1`, invitation.ID).Scan(&invitationXID); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT xmin::text FROM audit_events WHERE request_id=$1 AND action='invitation.create'`, invitationRequestID).Scan(&invitationAuditXID); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT xmin::text FROM outbox WHERE payload_json->>'invitation_id'=$1 AND topic='invitation.created'`, invitation.ID).Scan(&invitationOutboxXID); err != nil {
		t.Fatal(err)
	}
	if invitationXID != invitationAuditXID || invitationXID != invitationOutboxXID {
		t.Fatalf("invitation rows used different transactions: invitation=%s audit=%s outbox=%s", invitationXID, invitationAuditXID, invitationOutboxXID)
	}

	claimRequestID := "atomic-claim-" + uuid.NewString()
	inviteeID := "atomic-invitee|" + uuid.NewString()
	claimCtx := auth.WithRequestID(auth.WithPrincipal(ctx, auth.Principal{UserID: inviteeID, Issuer: "atomic-invitee", Subject: inviteeID, Email: invitation.Email, EmailVerified: true}), claimRequestID)
	if _, err = service.ClaimInvitation(claimCtx, invitation.ID); err != nil {
		t.Fatal(err)
	}
	var claimedInvitationXID, membershipXID, claimAuditXID, claimOutboxXID string
	if err = store.Pool.QueryRow(ctx, `SELECT xmin::text FROM workspace_invitations WHERE id=$1`, invitation.ID).Scan(&claimedInvitationXID); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT xmin::text FROM memberships WHERE workspace_id=$1 AND user_id=$2`, created.ID, inviteeID).Scan(&membershipXID); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT xmin::text FROM audit_events WHERE request_id=$1 AND action='invitation.claim'`, claimRequestID).Scan(&claimAuditXID); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT xmin::text FROM outbox WHERE payload_json->>'invitation_id'=$1 AND topic='invitation.claimed'`, invitation.ID).Scan(&claimOutboxXID); err != nil {
		t.Fatal(err)
	}
	if claimedInvitationXID != membershipXID || claimedInvitationXID != claimAuditXID || claimedInvitationXID != claimOutboxXID {
		t.Fatalf("claim rows used different transactions: invitation=%s membership=%s audit=%s outbox=%s", claimedInvitationXID, membershipXID, claimAuditXID, claimOutboxXID)
	}
}

func TestWorkspaceDeletionPreservesAuditAndEnqueuesArtifactCleanupAtomically(t *testing.T) {
	url := os.Getenv("AGENTX_POSTGRES_TEST_URL")
	if url == "" {
		t.Skip("AGENTX_POSTGRES_TEST_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	applyTestMigrations(t, store, ctx)
	workspaceRepo := WorkspaceRepository{Store: store}
	packageRepo := PackageRepository{Store: store}
	auditRepo := AuditRepository{Store: store}
	outboxRepo := OutboxRepo{Store: store}
	ownerID := "delete-owner|" + uuid.NewString()
	createCtx := auth.WithPrincipal(ctx, auth.Principal{UserID: ownerID})
	service := workspaceusecase.New(workspaceRepo)
	service.SetArtifactLifecycle(packageRepo)
	service.SetAudit(auditRepo)
	service.SetUnitOfWork(store)
	workspace, err := service.Create(createCtx, "Lifecycle", "lifecycle-"+strings.ToLower(uuid.NewString()[:8]))
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("d", 64)
	if err = packageRepo.SaveForWorkspace(ctx, workspace.ID, entity.Release{Name: "lifecycle", Version: "1.0.0", SHA256: digest, Size: 10, Status: "published", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	service.SetOutbox(failingOutbox{})
	deleteRequestID := "delete-failed-" + uuid.NewString()
	deleteCtx := auth.WithRequestID(createCtx, deleteRequestID)
	if err = service.Delete(deleteCtx, workspace.ID); err == nil || !strings.Contains(err.Error(), "forced outbox failure") {
		t.Fatalf("expected deletion rollback, got %v", err)
	}
	var workspaceCount, failedAuditCount int
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE id=$1`, workspace.ID).Scan(&workspaceCount); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE request_id=$1`, deleteRequestID).Scan(&failedAuditCount); err != nil {
		t.Fatal(err)
	}
	if workspaceCount != 1 || failedAuditCount != 0 {
		t.Fatalf("failed deletion partially committed: workspace=%d audit=%d", workspaceCount, failedAuditCount)
	}

	service.SetOutbox(outboxRepo)
	deleteRequestID = "delete-success-" + uuid.NewString()
	deleteCtx = auth.WithRequestID(createCtx, deleteRequestID)
	if err = service.Delete(deleteCtx, workspace.ID); err != nil {
		t.Fatal(err)
	}
	var auditWorkspaceID *string
	var auditResourceID, auditXID string
	if err = store.Pool.QueryRow(ctx, `SELECT workspace_id::text,resource_id,xmin::text FROM audit_events WHERE request_id=$1 AND action='workspace.delete'`, deleteRequestID).Scan(&auditWorkspaceID, &auditResourceID, &auditXID); err != nil {
		t.Fatal(err)
	}
	if auditWorkspaceID != nil || auditResourceID != workspace.ID {
		t.Fatalf("deletion tombstone was not preserved safely: workspace=%v resource=%q", auditWorkspaceID, auditResourceID)
	}
	var cleanupXID, deletedXID, cleanupDigest string
	if err = store.Pool.QueryRow(ctx, `SELECT payload_json->>'digest',xmin::text FROM outbox WHERE payload_json->>'workspace_id'=$1 AND topic='artifact.cleanup'`, workspace.ID).Scan(&cleanupDigest, &cleanupXID); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT xmin::text FROM outbox WHERE payload_json->>'workspace_id'=$1 AND topic='workspace.deleted'`, workspace.ID).Scan(&deletedXID); err != nil {
		t.Fatal(err)
	}
	if cleanupDigest != digest || auditXID != cleanupXID || auditXID != deletedXID {
		t.Fatalf("deletion lifecycle rows differ: digest=%s audit=%s cleanup=%s deleted=%s", cleanupDigest, auditXID, cleanupXID, deletedXID)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE id=$1`, workspace.ID).Scan(&workspaceCount); err != nil || workspaceCount != 0 {
		t.Fatalf("workspace deletion did not commit: count=%d err=%v", workspaceCount, err)
	}
}

func TestPostgresLegalHoldsEnforceDeletionAndAuditRetention(t *testing.T) {
	url := os.Getenv("AGENTX_POSTGRES_TEST_URL")
	if url == "" {
		t.Skip("AGENTX_POSTGRES_TEST_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	applyTestMigrations(t, store, ctx)

	workspaceRepo := WorkspaceRepository{Store: store}
	holdRepo := LegalHoldRepository{Store: store}
	accountRepo := AccountRepository{Store: store}
	retentionRepo := AuditRetentionRepository{Store: store}
	now := time.Now().UTC()
	ownerID := "hold-owner|" + uuid.NewString()
	accountID := "hold-account|" + uuid.NewString()
	workspace := entity.Workspace{ID: uuid.NewString(), Slug: "hold-" + strings.ToLower(uuid.NewString()[:8]), Name: "Held Workspace", CreatedAt: now}
	if err = workspaceRepo.Create(ctx, workspace, entity.Membership{WorkspaceID: workspace.ID, UserID: ownerID, Role: entity.RoleOwner, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err = workspaceRepo.AddMember(ctx, entity.Membership{WorkspaceID: workspace.ID, UserID: accountID, Role: entity.RoleViewer, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	failedHoldID := uuid.NewString()
	failedAudit := entity.AuditEvent{Action: "legal_hold.create", ActorID: ownerID, ResourceType: "legal_hold", ResourceID: failedHoldID, At: now}
	err = holdRepo.CreateLegalHold(ctx, entity.LegalHold{ID: failedHoldID, TargetType: entity.LegalHoldTargetAccount, TargetID: accountID, Reason: "rollback", CreatedBy: ownerID, CreatedAt: now}, failedAudit, entity.OutboxEvent{ID: "not-a-uuid", Topic: "legal_hold.created", Payload: map[string]any{}, AvailableAt: now})
	if err == nil {
		t.Fatal("expected invalid outbox ID to roll back legal hold creation")
	}
	var failedRows int
	if err = store.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM legal_holds WHERE id::text=$1)+(SELECT count(*) FROM audit_events WHERE resource_id=$1)`, failedHoldID).Scan(&failedRows); err != nil || failedRows != 0 {
		t.Fatalf("failed legal hold partially committed: rows=%d err=%v", failedRows, err)
	}

	createHold := func(targetType, targetID string) entity.LegalHold {
		hold := entity.LegalHold{ID: uuid.NewString(), TargetType: targetType, TargetID: targetID, Reason: "preservation required", CreatedBy: ownerID, CreatedAt: now}
		audit := entity.AuditEvent{Action: "legal_hold.create", ActorID: ownerID, ResourceType: "legal_hold", ResourceID: hold.ID, Metadata: map[string]any{"target_type": targetType, "target_id": targetID}, At: now}
		outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "legal_hold.created", Payload: map[string]any{"legal_hold_id": hold.ID}, AvailableAt: now}
		if createErr := holdRepo.CreateLegalHold(ctx, hold, audit, outbox); createErr != nil {
			t.Fatal(createErr)
		}
		return hold
	}
	accountHold := createHold(entity.LegalHoldTargetAccount, accountID)
	workspaceHold := createHold(entity.LegalHoldTargetWorkspace, workspace.ID)
	var holdXID, holdAuditXID, holdOutboxXID string
	var platformAudit bool
	if err = store.Pool.QueryRow(ctx, `SELECT xmin::text FROM legal_holds WHERE id=$1`, accountHold.ID).Scan(&holdXID); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT xmin::text,workspace_id IS NULL FROM audit_events WHERE action='legal_hold.create' AND resource_id=$1`, accountHold.ID).Scan(&holdAuditXID, &platformAudit); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT xmin::text FROM outbox WHERE topic='legal_hold.created' AND payload_json->>'legal_hold_id'=$1`, accountHold.ID).Scan(&holdOutboxXID); err != nil {
		t.Fatal(err)
	}
	if holdXID != holdAuditXID || holdXID != holdOutboxXID || !platformAudit {
		t.Fatalf("legal hold was not atomically recorded as platform audit: hold=%s audit=%s outbox=%s platform=%v", holdXID, holdAuditXID, holdOutboxXID, platformAudit)
	}

	if err = accountRepo.DeleteAccount(ctx, entity.AccountDeletion{UserID: accountID, DeletionID: uuid.NewString(), At: now}); !errors.Is(err, repo.ErrConflict) {
		t.Fatalf("account deletion under hold should conflict, got %v", err)
	}
	if err = workspaceRepo.DeleteWorkspace(ctx, workspace.ID); !errors.Is(err, repo.ErrConflict) {
		t.Fatalf("workspace deletion under hold should conflict, got %v", err)
	}

	old := now.AddDate(0, 0, -400)
	requestPrefix := "retention-" + uuid.NewString()
	if _, err = store.Pool.Exec(ctx, `INSERT INTO audit_events(workspace_id,actor_id,action,request_id,created_at) VALUES($1,$2,'held.workspace',$3,$5),($1,$2,'held.account',$4,$5),(NULL,NULL,'expired.unheld',$6,$5),(NULL,NULL,'account.delete',$7,$5)`, workspace.ID, ownerID, requestPrefix+"-workspace", requestPrefix+"-account", old, requestPrefix+"-unheld", requestPrefix+"-tombstone"); err != nil {
		t.Fatal(err)
	}
	// Attribute one old event to the held account while keeping another protected by the held Workspace.
	if _, err = store.Pool.Exec(ctx, `UPDATE audit_events SET actor_id=$2 WHERE request_id=$1`, requestPrefix+"-account", accountID); err != nil {
		t.Fatal(err)
	}
	deleted, err := retentionRepo.PruneAuditEvents(ctx, now.AddDate(0, 0, -365), 100)
	if err != nil {
		t.Fatal(err)
	}
	if deleted < 1 {
		t.Fatalf("expected the unheld expired event to be pruned, deleted %d", deleted)
	}
	var preserved, unheld int
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE request_id IN ($1,$2,$3)`, requestPrefix+"-workspace", requestPrefix+"-account", requestPrefix+"-tombstone").Scan(&preserved); err != nil || preserved != 3 {
		t.Fatalf("held events or tombstone were pruned: count=%d err=%v", preserved, err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE request_id=$1`, requestPrefix+"-unheld").Scan(&unheld); err != nil || unheld != 0 {
		t.Fatalf("unheld expired event was retained: count=%d err=%v", unheld, err)
	}

	release := func(hold entity.LegalHold) {
		audit := entity.AuditEvent{Action: "legal_hold.release", ActorID: ownerID, ResourceType: "legal_hold", ResourceID: hold.ID, At: now.Add(time.Second)}
		outbox := entity.OutboxEvent{ID: uuid.NewString(), Topic: "legal_hold.released", Payload: map[string]any{"legal_hold_id": hold.ID}, AvailableAt: now.Add(time.Second)}
		if _, releaseErr := holdRepo.ReleaseLegalHold(ctx, hold.ID, ownerID, "matter closed", now.Add(time.Second), audit, outbox); releaseErr != nil {
			t.Fatal(releaseErr)
		}
	}
	release(accountHold)
	release(workspaceHold)
	if err = workspaceRepo.DeleteWorkspace(ctx, workspace.ID); err != nil {
		t.Fatalf("released workspace should be deletable: %v", err)
	}
	if err = accountRepo.DeleteAccount(ctx, entity.AccountDeletion{UserID: accountID, DeletionID: uuid.NewString(), At: now.Add(2 * time.Second)}); err != nil {
		t.Fatalf("released account should be deletable: %v", err)
	}
	var accountCount, activeHolds, retainedIdentifiers int
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id=$1`, accountID).Scan(&accountCount); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT count(*) FROM legal_holds WHERE id IN ($1,$2) AND released_at IS NULL`, accountHold.ID, workspaceHold.ID).Scan(&activeHolds); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM legal_holds WHERE target_id=$1 OR created_by=$1 OR released_by=$1)+(SELECT count(*) FROM audit_events WHERE actor_id=$1 OR metadata_json::text LIKE '%'||$1||'%')+(SELECT count(*) FROM outbox WHERE payload_json::text LIKE '%'||$1||'%')`, accountID).Scan(&retainedIdentifiers); err != nil {
		t.Fatal(err)
	}
	if accountCount != 0 || activeHolds != 0 || retainedIdentifiers != 0 {
		t.Fatalf("release/deletion state is inconsistent: account=%d active_holds=%d retained_identifiers=%d", accountCount, activeHolds, retainedIdentifiers)
	}
}
