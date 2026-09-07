package file

import (
	"agentx/server/internal/entity"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type PackageRepo struct {
	path string
}

type storedRelease struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	entity.Release
}

func NewPackageRepo(dir string) *PackageRepo {
	return &PackageRepo{path: filepath.Join(dir, "packages.json")}
}
func (r *PackageRepo) List(_ context.Context) ([]entity.Release, error) {
	dataMu.Lock()
	defer dataMu.Unlock()
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
	dataMu.Lock()
	defer dataMu.Unlock()
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
	dataMu.Lock()
	defer dataMu.Unlock()
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
	dataMu.Lock()
	defer dataMu.Unlock()
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
	dataMu.Lock()
	defer dataMu.Unlock()
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
	dataMu.Lock()
	defer dataMu.Unlock()
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
	dataMu.Lock()
	defer dataMu.Unlock()
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
	dataMu.Lock()
	defer dataMu.Unlock()
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

type idempotencyEntry struct {
	Fingerprint string         `json:"fingerprint,omitempty"`
	Release     entity.Release `json:"release"`
}

func (r *PackageRepo) LookupIdempotency(ctx context.Context, workspaceID, key string) (entity.Release, bool, error) {
	v, found, _, err := r.LookupIdempotencyFingerprint(ctx, workspaceID, key)
	return v, found, err
}

func (r *PackageRepo) LookupIdempotencyFingerprint(_ context.Context, workspaceID, key string) (entity.Release, bool, string, error) {
	dataMu.Lock()
	defer dataMu.Unlock()
	entries, err := r.loadIdempotency()
	if err != nil {
		return entity.Release{}, false, "", err
	}
	entry, ok := entries[workspaceID+"\x00"+key]
	if !ok {
		return entity.Release{}, false, "", nil
	}
	return entry.Release, true, entry.Fingerprint, nil
}

func (r *PackageRepo) StoreIdempotency(ctx context.Context, workspaceID, key string, v entity.Release) error {
	return r.StoreIdempotencyFingerprint(ctx, workspaceID, key, "", v)
}

func (r *PackageRepo) StoreIdempotencyFingerprint(_ context.Context, workspaceID, key, fingerprint string, v entity.Release) error {
	dataMu.Lock()
	defer dataMu.Unlock()
	entries, err := r.loadIdempotency()
	if err != nil {
		return err
	}
	key = workspaceID + "\x00" + key
	if existing, ok := entries[key]; ok {
		if existing.Fingerprint != "" && fingerprint != "" && existing.Fingerprint != fingerprint {
			return fmt.Errorf("idempotency key was already used for a different request")
		}
		return nil
	}
	entries[key] = idempotencyEntry{Fingerprint: fingerprint, Release: v}
	return r.saveIdempotency(entries)
}

func (r *PackageRepo) loadIdempotency() (map[string]idempotencyEntry, error) {
	b, err := os.ReadFile(r.path + ".idempotency.json")
	if os.IsNotExist(err) {
		return map[string]idempotencyEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err = json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	entries := make(map[string]idempotencyEntry, len(raw))
	for key, value := range raw {
		var entry idempotencyEntry
		if err := json.Unmarshal(value, &entry); err == nil && entry.Release.Name != "" {
			entries[key] = entry
			continue
		}
		var legacy entity.Release
		if err := json.Unmarshal(value, &legacy); err != nil {
			return nil, err
		}
		entries[key] = idempotencyEntry{Release: legacy}
	}
	return entries, nil
}

func (r *PackageRepo) saveIdempotency(entries map[string]idempotencyEntry) error {
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
