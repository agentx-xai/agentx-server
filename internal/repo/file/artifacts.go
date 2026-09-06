package file

import (
	"agentx/server/internal/entity"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type ArtifactStore struct{ dir string }

const maxArtifactSize int64 = 51 << 20

func NewArtifactStore(dir string) *ArtifactStore {
	return &ArtifactStore{dir: filepath.Join(dir, "artifacts")}
}
func (s *ArtifactStore) Ready(_ context.Context) error { return os.MkdirAll(s.dir, 0750) }
func (s *ArtifactStore) Put(_ context.Context, name string, src io.Reader) (entity.Release, error) {
	if name == "" {
		return entity.Release{}, fmt.Errorf("package name is required")
	}
	if err := os.MkdirAll(s.dir, 0750); err != nil {
		return entity.Release{}, err
	}
	tmp, err := os.CreateTemp(s.dir, "upload-")
	if err != nil {
		return entity.Release{}, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(src, maxArtifactSize+1))
	if err != nil {
		tmp.Close()
		return entity.Release{}, err
	}
	if n > maxArtifactSize {
		_ = tmp.Close()
		return entity.Release{}, fmt.Errorf("artifact exceeds %d bytes", maxArtifactSize)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return entity.Release{}, err
	}
	if err := tmp.Close(); err != nil {
		return entity.Release{}, err
	}
	digest := fmt.Sprintf("%x", h.Sum(nil))
	out := filepath.Join(s.dir, digest)
	if info, err := os.Lstat(out); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return entity.Release{}, fmt.Errorf("artifact path is not a regular file")
		}
		return entity.Release{SHA256: digest, Size: info.Size()}, nil
	} else if !os.IsNotExist(err) {
		return entity.Release{}, err
	}
	if err := os.Rename(tmp.Name(), out); err != nil && !os.IsExist(err) {
		return entity.Release{}, err
	}
	return entity.Release{Name: name, SHA256: digest, Size: n}, nil
}
func (s *ArtifactStore) Open(_ context.Context, digest string) (io.ReadCloser, error) {
	if filepath.Base(digest) != digest {
		return nil, fmt.Errorf("invalid digest")
	}
	path := filepath.Join(s.dir, digest)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("invalid artifact")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != digest {
		return nil, fmt.Errorf("artifact digest mismatch")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
func (s *ArtifactStore) Delete(_ context.Context, digest string) error {
	if filepath.Base(digest) != digest {
		return fmt.Errorf("invalid digest")
	}
	if err := os.Remove(filepath.Join(s.dir, digest)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
func (s *ArtifactStore) Stat(_ context.Context, digest string) (entity.Release, error) {
	if filepath.Base(digest) != digest {
		return entity.Release{}, fmt.Errorf("invalid digest")
	}
	info, err := os.Lstat(filepath.Join(s.dir, digest))
	if err != nil {
		return entity.Release{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return entity.Release{}, fmt.Errorf("invalid artifact")
	}
	return entity.Release{SHA256: digest, Size: info.Size()}, nil
}
