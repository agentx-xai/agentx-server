package file

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"errors"
	"testing"
	"time"
)

func TestWorkspaceRepositoryErrorContracts(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	repository := NewWorkspaceRepo(t.TempDir())
	workspace := entity.Workspace{ID: "workspace-1", Slug: "platform", Name: "Platform", CreatedAt: now}
	owner := entity.Membership{WorkspaceID: workspace.ID, UserID: "issuer|owner", Role: entity.RoleOwner, CreatedAt: now}
	if err := repository.Create(ctx, workspace, owner); err != nil {
		t.Fatal(err)
	}
	if err := repository.Create(ctx, entity.Workspace{ID: "workspace-2", Slug: workspace.Slug, Name: "Duplicate", CreatedAt: now}, entity.Membership{WorkspaceID: "workspace-2", UserID: owner.UserID, Role: entity.RoleOwner, CreatedAt: now}); !errors.Is(err, repo.ErrConflict) {
		t.Fatalf("duplicate slug must be a conflict, got %v", err)
	}
	if err := repository.AddMember(ctx, owner); !errors.Is(err, repo.ErrConflict) {
		t.Fatalf("duplicate membership must be a conflict, got %v", err)
	}
	if _, err := repository.Membership(ctx, workspace.ID, "issuer|missing"); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("missing membership must be not found, got %v", err)
	}
	if err := repository.UpdateMemberRole(ctx, workspace.ID, "issuer|missing", entity.RoleViewer); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("missing membership update must be not found, got %v", err)
	}
	if err := repository.RemoveMember(ctx, workspace.ID, "issuer|missing"); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("missing membership removal must be not found, got %v", err)
	}
	if err := repository.DeleteWorkspace(ctx, "workspace-missing"); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("missing workspace deletion must be not found, got %v", err)
	}
}

func TestPackageRepositoryErrorContracts(t *testing.T) {
	ctx := context.Background()
	repository := NewPackageRepo(t.TempDir())
	release := entity.Release{Name: "demo", Version: "1.0.0", SHA256: "digest", Status: "pending_approval", CreatedAt: time.Now().UTC()}
	if err := repository.SaveForWorkspace(ctx, "workspace-1", release); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveForWorkspace(ctx, "workspace-1", release); !errors.Is(err, repo.ErrConflict) {
		t.Fatalf("duplicate release must be a conflict, got %v", err)
	}
	if _, err := repository.FindReleaseByDigestForWorkspace(ctx, "workspace-1", "missing"); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("missing artifact must be not found, got %v", err)
	}
	if _, err := repository.FindReleaseForWorkspace(ctx, "workspace-1", "demo", "2.0.0"); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("missing release must be not found, got %v", err)
	}
	if _, err := repository.ApproveReleaseForWorkspace(ctx, "workspace-1", "demo", "2.0.0"); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("missing approval target must be not found, got %v", err)
	}
	if _, err := repository.ApproveReleaseForWorkspace(ctx, "workspace-1", "demo", "1.0.0"); err != nil {
		t.Fatalf("pending release approval failed: %v", err)
	}
	if _, err := repository.ApproveReleaseForWorkspace(ctx, "workspace-1", "demo", "1.0.0"); !errors.Is(err, repo.ErrConflict) {
		t.Fatalf("repeated approval must be a conflict, got %v", err)
	}
}
