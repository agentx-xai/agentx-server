package manifest

import (
	"agentx/server/internal/apperror"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo/file"
	"agentx/server/internal/usecase/workspace"
	"context"
	"testing"
)

const manifestDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func manifestFixture(t *testing.T) (*Service, *file.PackageRepo, string) {
	t.Helper()
	dir := t.TempDir()
	workspaces := workspace.New(file.NewWorkspaceRepo(dir))
	created, err := workspaces.Create(context.Background(), "Platform", "platform")
	if err != nil {
		t.Fatal(err)
	}
	packages := file.NewPackageRepo(dir)
	service := New(file.NewWorkspaceRepo(dir), workspaces)
	service.SetPackages(packages)
	return service, packages, created.ID
}

func TestReplaceRejectsMalformedAndUnknownManifestFields(t *testing.T) {
	service, _, workspaceID := manifestFixture(t)
	_, err := service.Replace(context.Background(), workspaceID, map[string]any{
		"version":  1,
		"packages": []any{map[string]any{"name": "demo", "sha256": "bad"}},
	})
	if kind, _, ok := apperror.Public(err); !ok || kind != apperror.KindValidation {
		t.Fatalf("expected validation error, got %v", err)
	}
	current, currentErr := service.Current(context.Background(), workspaceID)
	if currentErr != nil || current.Revision != 0 {
		t.Fatalf("invalid manifest was persisted: %+v %v", current, currentErr)
	}
}

func TestReplaceRequiresMatchingDownloadableWorkspaceRelease(t *testing.T) {
	service, packages, workspaceID := manifestFixture(t)
	document := map[string]any{
		"version":  1,
		"packages": []any{map[string]any{"name": "demo", "version": "1.0.0", "sha256": manifestDigest}},
	}
	if _, err := service.Replace(context.Background(), workspaceID, document); err == nil {
		t.Fatal("missing release was accepted")
	}
	if err := packages.SaveForWorkspace(context.Background(), workspaceID, entity.Release{Name: "demo", Version: "1.0.0", SHA256: manifestDigest, Status: "pending_approval"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Replace(context.Background(), workspaceID, document); err == nil {
		t.Fatal("pending release was accepted")
	}
	approved, err := packages.ApproveReleaseForWorkspace(context.Background(), workspaceID, "demo", "1.0.0")
	if err != nil || approved.Status != "approved" {
		t.Fatalf("approve release: %+v %v", approved, err)
	}
	stored, err := service.Replace(context.Background(), workspaceID, document)
	if err != nil || stored.Revision != 1 {
		t.Fatalf("replace valid manifest: %+v %v", stored, err)
	}
}

func TestReplaceAcceptsExplicitEmptyDesiredState(t *testing.T) {
	service, _, workspaceID := manifestFixture(t)
	stored, err := service.Replace(context.Background(), workspaceID, map[string]any{
		"version": 1, "packages": []any{},
	})
	if err != nil || stored.Revision != 1 {
		t.Fatalf("replace empty manifest: %+v %v", stored, err)
	}
}
