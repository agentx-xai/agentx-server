package file

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtifactStoreRoundTrip(t *testing.T) {
	s := NewArtifactStore(t.TempDir())
	r, e := s.Put(context.Background(), "demo", strings.NewReader("hello"))
	if e != nil {
		t.Fatal(e)
	}
	f, e := s.Open(context.Background(), r.SHA256)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	b, e := io.ReadAll(f)
	if e != nil || string(b) != "hello" {
		t.Fatalf("got %q err %v", b, e)
	}
}

func TestArtifactStoreDeduplicationPreservesRequestedName(t *testing.T) {
	s := NewArtifactStore(t.TempDir())
	if _, err := s.Put(context.Background(), "first", strings.NewReader("shared")); err != nil {
		t.Fatal(err)
	}
	second, err := s.Put(context.Background(), "second", strings.NewReader("shared"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Name != "second" {
		t.Fatalf("deduplicated artifact lost package name: %+v", second)
	}
}
func TestArtifactStoreRejectsTraversal(t *testing.T) {
	s := NewArtifactStore(t.TempDir())
	if _, e := s.Open(context.Background(), "../secret"); e == nil {
		t.Fatal("expected traversal rejection")
	}
}
func TestArtifactStoreRejectsOversizedUpload(t *testing.T) {
	s := NewArtifactStore(t.TempDir())
	if _, err := s.Put(context.Background(), "demo", io.LimitReader(strings.NewReader(strings.Repeat("x", 51<<20+1)), 51<<20+1)); err == nil {
		t.Fatal("expected oversized artifact rejection")
	}
}
func TestArtifactStoreDeleteAndStat(t *testing.T) {
	s := NewArtifactStore(t.TempDir())
	v, err := s.Put(context.Background(), "demo", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	stat, err := s.Stat(context.Background(), v.SHA256)
	if err != nil || stat.Size != 5 {
		t.Fatalf("stat: %+v %v", stat, err)
	}
	if err = s.Delete(context.Background(), v.SHA256); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Open(context.Background(), v.SHA256); err == nil {
		t.Fatal("expected deleted artifact")
	}
}

func TestArtifactStoreRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	store := NewArtifactStore(dir)
	if err := os.MkdirAll(filepath.Join(dir, "artifacts"), 0750); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(dir, "secret")
	if err := os.WriteFile(secret, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "artifacts", "digest")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(context.Background(), "digest"); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestArtifactStoreRejectsTamperedContent(t *testing.T) {
	dir := t.TempDir()
	store := NewArtifactStore(dir)
	value, err := store.Put(context.Background(), "demo", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "artifacts", value.SHA256), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(context.Background(), value.SHA256); err == nil {
		t.Fatal("expected digest mismatch")
	}
}
