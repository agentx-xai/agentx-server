package registry

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo/file"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type verifier struct{ ok bool }

func (v verifier) Verify(context.Context, []byte, string) error {
	if !v.ok {
		return errors.New("invalid")
	}
	return nil
}

func TestWorkspacePolicyRequiresApprovalAndBlocksDownload(t *testing.T) {
	dir := t.TempDir()
	packages := file.NewPackageRepo(dir)
	workspaceRepo := file.NewWorkspaceRepo(dir)
	workspaceID := "workspace-policy"
	if err := workspaceRepo.SavePolicy(context.Background(), entity.Policy{ID: "policy-1", WorkspaceID: workspaceID, Revision: 1, Document: map[string]any{"require_signature": true, "require_approval": true}, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	s := New(file.NewArtifactStore(dir), packages, verifier{ok: true})
	s.SetPolicy(workspaceRepo)
	if _, err := s.PublishForWorkspace(context.Background(), workspaceID, "demo", "1.0.0", strings.NewReader("x")); err == nil {
		t.Fatal("expected policy signature requirement")
	}
	v, err := s.PublishForWorkspace(context.Background(), workspaceID, "demo", "1.0.0", strings.NewReader("x"), "c2ln")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "pending_approval" {
		t.Fatalf("expected pending approval, got %q", v.Status)
	}
	if _, err := s.OpenReleaseForWorkspace(context.Background(), workspaceID, "demo", "1.0.0"); err == nil {
		t.Fatal("expected pending release download to be blocked")
	}
	if _, err := s.ApproveForWorkspace(context.Background(), workspaceID, "demo", "1.0.0"); err == nil {
		t.Fatal("expected approval authorization dependency")
	}
}

func TestPublishRejectsInvalidVersion(t *testing.T) {
	s := New(file.NewArtifactStore(t.TempDir()), file.NewPackageRepo(t.TempDir()))
	if _, e := s.Publish(context.Background(), "demo", "latest", strings.NewReader("x")); e == nil {
		t.Fatal("expected invalid version")
	}
}

func TestPublishRequiresAndRecordsSignature(t *testing.T) {
	dir := t.TempDir()
	s := New(file.NewArtifactStore(dir), file.NewPackageRepo(dir), verifier{ok: true})
	if _, err := s.Publish(context.Background(), "demo", "1.0.0", strings.NewReader("x")); err == nil {
		t.Fatal("expected signature requirement")
	}
	v, err := s.Publish(context.Background(), "demo", "1.0.0", strings.NewReader("x"), "c2ln")
	if err != nil {
		t.Fatal(err)
	}
	if v.SignatureStatus != "verified" || v.Signature != "c2ln" {
		t.Fatalf("signature metadata missing: %+v", v)
	}
}
func TestInvalidSignatureRemovesStoredArtifact(t *testing.T) {
	dir := t.TempDir()
	store := file.NewArtifactStore(dir)
	s := New(store, file.NewPackageRepo(dir), verifier{ok: false})
	if _, err := s.Publish(context.Background(), "demo", "1.0.0", strings.NewReader("x"), "c2ln"); err == nil {
		t.Fatal("expected invalid signature")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "artifacts", "*"))
	if len(files) != 0 {
		t.Fatalf("invalid artifact remained: %v", files)
	}
}
