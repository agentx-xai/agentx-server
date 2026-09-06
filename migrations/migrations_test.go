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
