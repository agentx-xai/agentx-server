package file

import (
	"agentx/server/internal/entity"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type WorkspaceRepo struct {
	path        string
	workspaces  []entity.Workspace
	memberships []entity.Membership
	policies    []entity.Policy
	manifests   []entity.TeamManifest
}

type workspaceData struct {
	Workspaces  []entity.Workspace    `json:"workspaces"`
	Memberships []entity.Membership   `json:"memberships"`
	Policies    []entity.Policy       `json:"policies,omitempty"`
	Manifests   []entity.TeamManifest `json:"manifests,omitempty"`
}

func NewWorkspaceRepo(dir string) *WorkspaceRepo {
	return &WorkspaceRepo{path: filepath.Join(dir, "workspaces.json")}
}
func (r *WorkspaceRepo) load() error {
	b, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var d workspaceData
	if err := json.Unmarshal(b, &d); err != nil {
		return err
	}
	r.workspaces, r.memberships, r.policies, r.manifests = d.Workspaces, d.Memberships, d.Policies, d.Manifests
	return nil
}
func (r *WorkspaceRepo) persist() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(workspaceData{Workspaces: r.workspaces, Memberships: r.memberships, Policies: r.policies, Manifests: r.manifests}, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err = os.WriteFile(tmp, b, 0640); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}
func (r *WorkspaceRepo) List(_ context.Context, userID string) ([]entity.Workspace, error) {
	dataMu.Lock()
	defer dataMu.Unlock()
	if err := r.load(); err != nil {
		return nil, err
	}
	var out []entity.Workspace
	for _, m := range r.memberships {
		if m.UserID == userID {
			for _, w := range r.workspaces {
				if w.ID == m.WorkspaceID {
					out = append(out, w)
				}
			}
		}
	}
	return out, nil
}
func (r *WorkspaceRepo) Create(_ context.Context, w entity.Workspace, m entity.Membership) error {
	dataMu.Lock()
	defer dataMu.Unlock()
	if err := r.load(); err != nil {
		return err
	}
	for _, existing := range r.workspaces {
		if existing.Slug == w.Slug {
			return errors.New("workspace slug already exists")
		}
	}
	r.workspaces = append(r.workspaces, w)
	r.memberships = append(r.memberships, m)
	return r.persist()
}
func (r *WorkspaceRepo) Membership(_ context.Context, workspaceID, userID string) (entity.Membership, error) {
	dataMu.Lock()
	defer dataMu.Unlock()
	if err := r.load(); err != nil {
		return entity.Membership{}, err
	}
	for _, m := range r.memberships {
		if m.WorkspaceID == workspaceID && m.UserID == userID {
			return m, nil
		}
	}
	return entity.Membership{}, errors.New("membership not found")
}
func (r *WorkspaceRepo) Members(_ context.Context, workspaceID string) ([]entity.Membership, error) {
	dataMu.Lock()
	defer dataMu.Unlock()
	if err := r.load(); err != nil {
		return nil, err
	}
	var out []entity.Membership
	for _, m := range r.memberships {
		if m.WorkspaceID == workspaceID {
			out = append(out, m)
		}
	}
	return out, nil
}
func (r *WorkspaceRepo) AddMember(_ context.Context, m entity.Membership) error {
	dataMu.Lock()
	defer dataMu.Unlock()
	if err := r.load(); err != nil {
		return err
	}
	for _, x := range r.memberships {
		if x.WorkspaceID == m.WorkspaceID && x.UserID == m.UserID {
			return errors.New("membership already exists")
		}
	}
	r.memberships = append(r.memberships, m)
	return r.persist()
}
func (r *WorkspaceRepo) UpdateMemberRole(_ context.Context, workspaceID, userID string, role entity.Role) error {
	dataMu.Lock()
	defer dataMu.Unlock()
	if err := r.load(); err != nil {
		return err
	}
	for i, m := range r.memberships {
		if m.WorkspaceID == workspaceID && m.UserID == userID {
			r.memberships[i].Role = role
			return r.persist()
		}
	}
	return errors.New("membership not found")
}
func (r *WorkspaceRepo) RemoveMember(_ context.Context, workspaceID, userID string) error {
	dataMu.Lock()
	defer dataMu.Unlock()
	if err := r.load(); err != nil {
		return err
	}
	for i, m := range r.memberships {
		if m.WorkspaceID == workspaceID && m.UserID == userID {
			r.memberships = append(r.memberships[:i], r.memberships[i+1:]...)
			return r.persist()
		}
	}
	return errors.New("membership not found")
}

func (r *WorkspaceRepo) DeleteWorkspace(_ context.Context, workspaceID string) error {
	dataMu.Lock()
	defer dataMu.Unlock()
	if err := r.load(); err != nil {
		return err
	}
	found := false
	workspaces := r.workspaces[:0]
	for _, item := range r.workspaces {
		if item.ID == workspaceID {
			found = true
			continue
		}
		workspaces = append(workspaces, item)
	}
	if !found {
		return errors.New("workspace not found")
	}
	r.workspaces = workspaces
	memberships := r.memberships[:0]
	for _, item := range r.memberships {
		if item.WorkspaceID != workspaceID {
			memberships = append(memberships, item)
		}
	}
	r.memberships = memberships
	policies := r.policies[:0]
	for _, item := range r.policies {
		if item.WorkspaceID != workspaceID {
			policies = append(policies, item)
		}
	}
	r.policies = policies
	manifests := r.manifests[:0]
	for _, item := range r.manifests {
		if item.WorkspaceID != workspaceID {
			manifests = append(manifests, item)
		}
	}
	r.manifests = manifests
	if err := r.cleanupWorkspaceData(workspaceID); err != nil {
		return err
	}
	return r.persist()
}

// Workspace-owned data lives in separate files in the file backend. Keep
// deletion equivalent to the PostgreSQL foreign-key cascades.
func (r *WorkspaceRepo) cleanupWorkspaceData(workspaceID string) error {
	if err := filterJSONFile(filepath.Join(filepath.Dir(r.path), "devices.json"), func(raw []byte) ([]byte, error) {
		var items []storedDevice
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		out := items[:0]
		for _, item := range items {
			if item.WorkspaceID != workspaceID {
				out = append(out, item)
			}
		}
		return json.MarshalIndent(out, "", "  ")
	}); err != nil {
		return err
	}
	if err := filterJSONFile(filepath.Join(filepath.Dir(r.path), "packages.json"), func(raw []byte) ([]byte, error) {
		var items []storedRelease
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		out := items[:0]
		for _, item := range items {
			if item.WorkspaceID != workspaceID {
				out = append(out, item)
			}
		}
		return json.MarshalIndent(out, "", "  ")
	}); err != nil {
		return err
	}
	if err := filterJSONFile(filepath.Join(filepath.Dir(r.path), "audit.json"), func(raw []byte) ([]byte, error) {
		var items []storedAuditEvent
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		out := items[:0]
		for _, item := range items {
			if item.WorkspaceID != workspaceID {
				out = append(out, item)
			}
		}
		return json.MarshalIndent(out, "", "  ")
	}); err != nil {
		return err
	}
	if err := filterJSONFile(filepath.Join(filepath.Dir(r.path), "packages.json.idempotency.json"), func(raw []byte) ([]byte, error) {
		var items map[string]idempotencyEntry
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		prefix := workspaceID + "\x00"
		for key := range items {
			if strings.HasPrefix(key, prefix) {
				delete(items, key)
			}
		}
		return json.MarshalIndent(items, "", "  ")
	}); err != nil {
		return err
	}
	return nil
}

func filterJSONFile(path string, transform func([]byte) ([]byte, error)) error {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	updated, err := transform(raw)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, updated, 0640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (r *WorkspaceRepo) CurrentPolicy(_ context.Context, workspaceID string) (entity.Policy, error) {
	dataMu.Lock()
	defer dataMu.Unlock()
	if err := r.load(); err != nil {
		return entity.Policy{}, err
	}
	var current entity.Policy
	for _, policy := range r.policies {
		if policy.WorkspaceID == workspaceID && policy.Revision >= current.Revision {
			current = policy
		}
	}
	if current.Revision == 0 {
		return entity.Policy{WorkspaceID: workspaceID, Revision: 0, Document: map[string]any{}}, nil
	}
	return current, nil
}

func (r *WorkspaceRepo) SavePolicy(_ context.Context, policy entity.Policy) error {
	dataMu.Lock()
	defer dataMu.Unlock()
	if err := r.load(); err != nil {
		return err
	}
	r.policies = append(r.policies, policy)
	return r.persist()
}

func (r *WorkspaceRepo) CurrentManifest(_ context.Context, workspaceID string) (entity.TeamManifest, error) {
	dataMu.Lock()
	defer dataMu.Unlock()
	if err := r.load(); err != nil {
		return entity.TeamManifest{}, err
	}
	var current entity.TeamManifest
	for _, manifest := range r.manifests {
		if manifest.WorkspaceID == workspaceID && manifest.Revision >= current.Revision {
			current = manifest
		}
	}
	if current.Revision == 0 {
		return entity.TeamManifest{WorkspaceID: workspaceID, Revision: 0, Document: map[string]any{}}, nil
	}
	return current, nil
}

func (r *WorkspaceRepo) SaveManifest(_ context.Context, manifest entity.TeamManifest) error {
	dataMu.Lock()
	defer dataMu.Unlock()
	if err := r.load(); err != nil {
		return err
	}
	r.manifests = append(r.manifests, manifest)
	return r.persist()
}
