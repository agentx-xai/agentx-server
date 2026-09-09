package registry

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo/file"
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

type verifier struct{ ok bool }

type failingWorkspaceAudit struct{ err error }

type concurrentArtifactStore struct {
	entered chan string
	release chan struct{}
}

func (s *concurrentArtifactStore) Put(_ context.Context, name string, input io.Reader) (entity.Release, error) {
	payload, err := io.ReadAll(input)
	if err != nil {
		return entity.Release{}, err
	}
	s.entered <- name
	<-s.release
	return entity.Release{Name: name, SHA256: fmt.Sprintf("%x", sha256.Sum256(payload)), Size: int64(len(payload)), CreatedAt: time.Now().UTC()}, nil
}
func (*concurrentArtifactStore) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}
func (*concurrentArtifactStore) Delete(context.Context, string) error { return nil }
func (*concurrentArtifactStore) Stat(context.Context, string) (entity.Release, error) {
	return entity.Release{}, errors.New("not implemented")
}

type atomicPackageRepository struct{}

func (atomicPackageRepository) List(context.Context) ([]entity.Release, error) { return nil, nil }
func (atomicPackageRepository) Save(context.Context, entity.Release) error     { return nil }
func (atomicPackageRepository) SaveForWorkspaceIdempotent(_ context.Context, _ string, _ string, record entity.IdempotencyRecord, _ entity.AuditEvent, _ entity.OutboxEvent) (entity.Release, bool, error) {
	return record.Release, false, nil
}

func (a failingWorkspaceAudit) AppendForWorkspace(context.Context, string, entity.AuditEvent) error {
	return a.err
}

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

func TestAtomicWorkspacePublishesAreNotGloballySerialized(t *testing.T) {
	store := &concurrentArtifactStore{entered: make(chan string, 2), release: make(chan struct{})}
	service := New(store, atomicPackageRepository{})
	errors := make(chan error, 2)
	for index, name := range []string{"alpha", "beta"} {
		go func(index int, name string) {
			_, _, err := service.PublishForWorkspaceIdempotent(
				context.Background(),
				"workspace",
				fmt.Sprintf("request-%d", index),
				name,
				"1.0.0",
				strings.NewReader(name),
			)
			errors <- err
		}(index, name)
	}
	for range 2 {
		select {
		case <-store.entered:
		case <-time.After(time.Second):
			close(store.release)
			t.Fatal("unrelated atomic uploads were serialized")
		}
	}
	close(store.release)
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorkspacePublishValidatesIdempotencyKeyInUseCase(t *testing.T) {
	service := New(file.NewArtifactStore(t.TempDir()), file.NewPackageRepo(t.TempDir()))
	if _, _, err := service.PublishForWorkspaceIdempotent(context.Background(), "workspace", "", "demo", "1.0.0", strings.NewReader("x")); err == nil {
		t.Fatal("empty idempotency key was accepted")
	}
	if _, _, err := service.PublishForWorkspaceIdempotent(context.Background(), "workspace", strings.Repeat("x", 256), "demo", "1.0.0", strings.NewReader("x")); err == nil {
		t.Fatal("oversized idempotency key was accepted")
	}
}

func TestWorkspacePublishReportsAuditFailure(t *testing.T) {
	dir := t.TempDir()
	packages := file.NewPackageRepo(dir)
	s := New(file.NewArtifactStore(dir), packages)
	s.SetAudit(failingWorkspaceAudit{err: errors.New("disk full")})
	v, err := s.PublishForWorkspace(context.Background(), "workspace-1", "demo", "1.0.0", strings.NewReader("x"))
	if err == nil || !strings.Contains(err.Error(), "release committed but audit append failed") {
		t.Fatalf("expected explicit audit failure, got %v", err)
	}
	items, listErr := packages.ListForWorkspace(context.Background(), "workspace-1")
	if listErr != nil || len(items) != 1 || items[0].SHA256 != v.SHA256 {
		t.Fatalf("committed release missing after reported audit failure: %+v, %v", items, listErr)
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
func TestInvalidSignatureDoesNotDeleteSharedArtifact(t *testing.T) {
	dir := t.TempDir()
	store := file.NewArtifactStore(dir)
	packages := file.NewPackageRepo(dir)
	valid := New(store, packages, verifier{ok: true})
	release, err := valid.Publish(context.Background(), "trusted", "1.0.0", strings.NewReader("x"), "c2ln")
	if err != nil {
		t.Fatal(err)
	}
	invalid := New(store, packages, verifier{ok: false})
	if _, err = invalid.Publish(context.Background(), "untrusted", "1.0.0", strings.NewReader("x"), "bad"); err == nil {
		t.Fatal("expected invalid signature")
	}
	reader, err := store.Open(context.Background(), release.SHA256)
	if err != nil {
		t.Fatalf("shared artifact was deleted: %v", err)
	}
	defer reader.Close()
	if data, err := io.ReadAll(reader); err != nil || string(data) != "x" {
		t.Fatalf("shared artifact changed: %q %v", data, err)
	}
}

type archiveEntry struct {
	name     string
	body     string
	typeflag byte
	mode     int64
}

func packageArchive(t *testing.T, entries ...archiveEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		kind := entry.typeflag
		if kind == 0 {
			kind = tar.TypeReg
		}
		mode := entry.mode
		if mode == 0 {
			mode = 0o644
		}
		header := &tar.Header{Name: entry.name, Typeflag: kind, Mode: mode, Size: int64(len(entry.body))}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if kind == tar.TypeReg {
			if _, err := tw.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestHostedPackageArchiveValidation(t *testing.T) {
	tests := []struct {
		name    string
		entries []archiveEntry
		wantErr string
	}{
		{"valid", []archiveEntry{{name: "SKILL.md", body: "# Demo"}, {name: "references/guide.md", body: "guide"}}, ""},
		{"missing manifest", []archiveEntry{{name: "README.md", body: "demo"}}, "SKILL.md"},
		{"traversal", []archiveEntry{{name: "SKILL.md", body: "# Demo"}, {name: "../secret", body: "secret"}}, "unsafe artifact path"},
		{"symlink", []archiveEntry{{name: "SKILL.md", body: "# Demo"}, {name: "link", typeflag: tar.TypeSymlink}}, "not a regular file"},
		{"credential", []archiveEntry{{name: "SKILL.md", body: "# Demo"}, {name: ".env.production", body: "TOKEN=x"}}, "forbidden credential"},
		{"executable", []archiveEntry{{name: "SKILL.md", body: "# Demo"}, {name: "run.sh", body: "exit 0", mode: 0o755}}, "executable file"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			service := New(file.NewArtifactStore(dir), file.NewPackageRepo(dir))
			service.SetPackageArchiveRequired(true)
			_, err := service.Publish(context.Background(), "demo", "1.0.0", bytes.NewReader(packageArchive(t, test.entries...)))
			if test.wantErr == "" && err != nil {
				t.Fatal(err)
			}
			if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("expected %q, got %v", test.wantErr, err)
			}
		})
	}
}
