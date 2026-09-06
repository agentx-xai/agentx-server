package file

import (
	"agentx/server/internal/entity"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type PackageRepo struct{ path string }

type storedRelease struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	entity.Release
}

func NewPackageRepo(dir string) *PackageRepo {
	return &PackageRepo{path: filepath.Join(dir, "packages.json")}
}
func (r *PackageRepo) List(_ context.Context) ([]entity.Release, error) {
	stored, err := r.load()
	if err != nil {
		return nil, err
	}
	out := make([]entity.Release, 0, len(stored))
	for _, item := range stored {
		out = append(out, item.Release)
	}
	return out, nil
}
func (r *PackageRepo) Save(ctx context.Context, p entity.Release) error {
	stored, err := r.load()
	if err != nil {
		return err
	}
	for _, existing := range stored {
		if existing.WorkspaceID == "" && existing.Name == p.Name && existing.Version == p.Version {
			return fmt.Errorf("release %s@%s already exists", p.Name, p.Version)
		}
	}
	return r.save(append(stored, storedRelease{Release: p}))
}
func (r *PackageRepo) ListForWorkspace(_ context.Context, workspaceID string) ([]entity.Release, error) {
	stored, err := r.load()
	if err != nil {
		return nil, err
	}
	out := make([]entity.Release, 0)
	for _, item := range stored {
		if item.WorkspaceID == workspaceID {
			out = append(out, item.Release)
		}
	}
	return out, nil
}
func (r *PackageRepo) SaveForWorkspace(_ context.Context, workspaceID string, p entity.Release) error {
	stored, err := r.load()
	if err != nil {
		return err
	}
	for _, existing := range stored {
		if existing.WorkspaceID == workspaceID && existing.Name == p.Name && existing.Version == p.Version {
			return fmt.Errorf("release %s@%s already exists", p.Name, p.Version)
		}
	}
	return r.save(append(stored, storedRelease{WorkspaceID: workspaceID, Release: p}))
}
func (r *PackageRepo) HasArtifactForWorkspace(_ context.Context, workspaceID, digest string) (bool, error) {
	stored, err := r.load()
	if err != nil {
		return false, err
	}
	for _, item := range stored {
		if item.WorkspaceID == workspaceID && item.SHA256 == digest {
			return true, nil
		}
	}
	return false, nil
}
func (r *PackageRepo) FindReleaseByDigestForWorkspace(_ context.Context, workspaceID, digest string) (entity.Release, error) {
	stored, err := r.load()
	if err != nil {
		return entity.Release{}, err
	}
	for _, item := range stored {
		if item.WorkspaceID == workspaceID && item.SHA256 == digest {
			return item.Release, nil
		}
	}
	return entity.Release{}, os.ErrNotExist
}
func (r *PackageRepo) FindReleaseForWorkspace(_ context.Context, workspaceID, name, version string) (entity.Release, error) {
	stored, err := r.load()
	if err != nil {
		return entity.Release{}, err
	}
	for _, item := range stored {
		if item.WorkspaceID == workspaceID && item.Name == name && item.Version == version {
			return item.Release, nil
		}
	}
	return entity.Release{}, os.ErrNotExist
}
func (r *PackageRepo) ApproveReleaseForWorkspace(_ context.Context, workspaceID, name, version string) (entity.Release, error) {
	stored, err := r.load()
	if err != nil {
		return entity.Release{}, err
	}
	for i, item := range stored {
		if item.WorkspaceID == workspaceID && item.Name == name && item.Version == version {
			stored[i].Status = "approved"
			if err := r.save(stored); err != nil {
				return entity.Release{}, err
			}
			return stored[i].Release, nil
		}
	}
	return entity.Release{}, os.ErrNotExist
}

func (r *PackageRepo) load() ([]storedRelease, error) {
	b, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return []storedRelease{}, nil
	}
	if err != nil {
		return nil, err
	}
	var stored []storedRelease
	if err := json.Unmarshal(b, &stored); err != nil {
		return nil, err
	}
	return stored, nil
}

func (r *PackageRepo) save(stored []storedRelease) error {
	b, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(r.path), 0750); err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err = os.WriteFile(tmp, b, 0640); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}
func (r *PackageRepo) LookupIdempotency(_ context.Context, workspaceID, key string) (entity.Release, bool, error) {
	b, err := os.ReadFile(r.path + ".idempotency.json")
	if os.IsNotExist(err) {
		return entity.Release{}, false, nil
	}
	if err != nil {
		return entity.Release{}, false, err
	}
	var entries map[string]entity.Release
	if err = json.Unmarshal(b, &entries); err != nil {
		return entity.Release{}, false, err
	}
	v, ok := entries[workspaceID+"\x00"+key]
	return v, ok, nil
}
func (r *PackageRepo) StoreIdempotency(_ context.Context, workspaceID, key string, v entity.Release) error {
	entries := map[string]entity.Release{}
	b, err := os.ReadFile(r.path + ".idempotency.json")
	if err == nil {
		if err = json.Unmarshal(b, &entries); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, ok := entries[workspaceID+"\x00"+key]; ok {
		return nil
	}
	entries[workspaceID+"\x00"+key] = v
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(r.path), 0750); err != nil {
		return err
	}
	tmp := r.path + ".idempotency.tmp"
	if err = os.WriteFile(tmp, raw, 0640); err != nil {
		return err
	}
	return os.Rename(tmp, r.path+".idempotency.json")
}
