package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestInitialSchemaContainsHostedDomainTables(t *testing.T) {
	b, err := os.ReadFile("001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	for _, table := range []string{"users", "workspaces", "memberships", "packages", "releases", "artifacts", "devices", "policies", "manifests", "audit_events", "outbox", "idempotency_keys"} {
		if !strings.Contains(s, "create table if not exists "+table) {
			t.Fatalf("migration missing %s", table)
		}
	}
	for _, constraint := range []string{"unique (workspace_id, name)", "unique (package_id, version)", "primary key (workspace_id, user_id)"} {
		if !strings.Contains(s, constraint) {
			t.Fatalf("migration missing constraint %s", constraint)
		}
	}
}

func TestWorkspaceScopedDeviceMigration(t *testing.T) {
	b, err := os.ReadFile("002_workspace_scoped_device_ids.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	for _, statement := range []string{
		"-- +goose up",
		"-- +goose down",
		"-- +goose statementbegin",
		"-- +goose statementend",
		"alter table devices alter column id type text",
		"primary key (workspace_id, id)",
		"foreign key (workspace_id, device_id)",
		"non-uuid device ids exist",
	} {
		if !strings.Contains(s, statement) {
			t.Fatalf("device migration missing %q", statement)
		}
	}
}

func TestSharedRateLimitMigration(t *testing.T) {
	b, err := os.ReadFile("003_shared_rate_limits.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	for _, statement := range []string{"-- +goose up", "create table if not exists rate_limit_windows", "primary key (identity, window_start)", "-- +goose down", "drop table if exists rate_limit_windows"} {
		if !strings.Contains(s, statement) {
			t.Fatalf("rate-limit migration missing %q", statement)
		}
	}
}

func TestIdempotencyResourceTypeMigration(t *testing.T) {
	b, err := os.ReadFile("004_idempotency_resource_types.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	for _, statement := range []string{"-- +goose up", "add column if not exists resource_type", "default 'release'", "-- +goose down", "drop column if exists resource_type"} {
		if !strings.Contains(s, statement) {
			t.Fatalf("idempotency migration missing %q", statement)
		}
	}
}

func TestWorkspaceInvitationMigration(t *testing.T) {
	b, err := os.ReadFile("005_workspace_invitations.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	for _, statement := range []string{
		"-- +goose up",
		"add column if not exists email_verified boolean not null default false",
		"create table if not exists workspace_invitations",
		"role in ('admin','developer','viewer')",
		"where revoked_at is null and accepted_at is null",
		"references workspaces(id) on delete cascade",
		"-- +goose down",
	} {
		if !strings.Contains(s, statement) {
			t.Fatalf("invitation migration missing %q", statement)
		}
	}
}

func TestWorkspaceDeletionAuditMigration(t *testing.T) {
	b, err := os.ReadFile("006_workspace_deletion_audit.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	for _, statement := range []string{"-- +goose up", "drop constraint if exists audit_events_workspace_id_fkey", "on delete set null", "-- +goose down", "on delete cascade"} {
		if !strings.Contains(s, statement) {
			t.Fatalf("workspace deletion audit migration missing %q", statement)
		}
	}
}

func TestAccountDeletionMigration(t *testing.T) {
	b, err := os.ReadFile("007_account_deletion.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	for _, statement := range []string{"-- +goose up", "alter column created_by drop not null", "on delete set null", "audit_events_actor_created_idx", "-- +goose down", "deleted invitation creators have been anonymized"} {
		if !strings.Contains(s, statement) {
			t.Fatalf("account deletion migration missing %q", statement)
		}
	}
}

func TestLegalHoldMigration(t *testing.T) {
	b, err := os.ReadFile("008_legal_holds.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	for _, statement := range []string{"-- +goose up", "create table legal_holds", "target_type in ('account','workspace')", "where released_at is null", "-- +goose down", "drop table if exists legal_holds"} {
		if !strings.Contains(s, statement) {
			t.Fatalf("legal-hold migration missing %q", statement)
		}
	}
}
