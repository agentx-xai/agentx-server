package file

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type WorkspaceRepo struct {
	path        string
	mu          sync.Mutex
	workspaces  []entity.Workspace
	memberships []entity.Membership
	invitations []entity.WorkspaceInvitation
	policies    []entity.Policy
	manifests   []entity.TeamManifest
}

type workspaceData struct {
	Workspaces  []entity.Workspace           `json:"workspaces"`
	Memberships []entity.Membership          `json:"memberships"`
	Invitations []entity.WorkspaceInvitation `json:"invitations,omitempty"`
	Policies    []entity.Policy              `json:"policies,omitempty"`
	Manifests   []entity.TeamManifest        `json:"manifests,omitempty"`
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
	r.workspaces, r.memberships, r.invitations, r.policies, r.manifests = d.Workspaces, d.Memberships, d.Invitations, d.Policies, d.Manifests
	return nil
}
func (r *WorkspaceRepo) persist() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(workspaceData{Workspaces: r.workspaces, Memberships: r.memberships, Invitations: r.invitations, Policies: r.policies, Manifests: r.manifests}, "", "  ")
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
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.load(); err != nil {
		return err
	}
	for _, existing := range r.workspaces {
		if existing.Slug == w.Slug {
			return repo.Conflict("workspace slug already exists")
		}
	}
	r.workspaces = append(r.workspaces, w)
	r.memberships = append(r.memberships, m)
	return r.persist()
}
func (r *WorkspaceRepo) Membership(_ context.Context, workspaceID, userID string) (entity.Membership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.load(); err != nil {
		return entity.Membership{}, err
	}
	for _, m := range r.memberships {
		if m.WorkspaceID == workspaceID && m.UserID == userID {
			return m, nil
		}
	}
	return entity.Membership{}, repo.NotFound("membership")
}
func (r *WorkspaceRepo) Members(_ context.Context, workspaceID string) ([]entity.Membership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.load(); err != nil {
		return err
	}
	for _, x := range r.memberships {
		if x.WorkspaceID == m.WorkspaceID && x.UserID == m.UserID {
			return repo.Conflict("membership already exists")
		}
	}
	r.memberships = append(r.memberships, m)
	return r.persist()
}
func (r *WorkspaceRepo) UpdateMemberRole(_ context.Context, workspaceID, userID string, role entity.Role) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.load(); err != nil {
		return err
	}
	for i, m := range r.memberships {
		if m.WorkspaceID == workspaceID && m.UserID == userID {
			r.memberships[i].Role = role
			return r.persist()
		}
	}
	return repo.NotFound("membership")
}
func (r *WorkspaceRepo) RemoveMember(_ context.Context, workspaceID, userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.load(); err != nil {
		return err
	}
	for i, m := range r.memberships {
		if m.WorkspaceID == workspaceID && m.UserID == userID {
			r.memberships = append(r.memberships[:i], r.memberships[i+1:]...)
			return r.persist()
		}
	}
	return repo.NotFound("membership")
}

func (r *WorkspaceRepo) CreateInvitation(_ context.Context, invitation entity.WorkspaceInvitation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.load(); err != nil {
		return err
	}
	workspaceFound := false
	for _, workspace := range r.workspaces {
		if workspace.ID == invitation.WorkspaceID {
			workspaceFound = true
			break
		}
	}
	if !workspaceFound {
		return repo.NotFound("workspace")
	}
	for index := range r.invitations {
		existing := &r.invitations[index]
		if existing.WorkspaceID != invitation.WorkspaceID || existing.Email != invitation.Email || existing.AcceptedAt != nil || existing.RevokedAt != nil {
			continue
		}
		if existing.ExpiresAt.After(invitation.CreatedAt) {
			return repo.Conflict("pending invitation already exists")
		}
		existing.RevokedAt = &invitation.CreatedAt
		existing.RevokedBy = invitation.CreatedBy
		existing.SetStatus(invitation.CreatedAt)
	}
	invitation.SetStatus(invitation.CreatedAt)
	r.invitations = append(r.invitations, invitation)
	return r.persist()
}

func (r *WorkspaceRepo) Invitation(_ context.Context, workspaceID, invitationID string) (entity.WorkspaceInvitation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.load(); err != nil {
		return entity.WorkspaceInvitation{}, err
	}
	for _, invitation := range r.invitations {
		if invitation.WorkspaceID == workspaceID && invitation.ID == invitationID {
			invitation.SetStatus(time.Now().UTC())
			return invitation, nil
		}
	}
	return entity.WorkspaceInvitation{}, repo.NotFound("invitation")
}

func (r *WorkspaceRepo) InvitationForEmail(_ context.Context, invitationID, email string) (entity.WorkspaceInvitation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.load(); err != nil {
		return entity.WorkspaceInvitation{}, err
	}
	for _, invitation := range r.invitations {
		if invitation.ID == invitationID && invitation.Email == email {
			invitation.SetStatus(time.Now().UTC())
			return invitation, nil
		}
	}
	return entity.WorkspaceInvitation{}, repo.NotFound("invitation")
}

func (r *WorkspaceRepo) InvitationsForWorkspacePage(_ context.Context, workspaceID string, request repo.PageRequest) (repo.Page[entity.WorkspaceInvitation], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.load(); err != nil {
		return repo.Page[entity.WorkspaceInvitation]{}, err
	}
	now := time.Now().UTC()
	items := make([]entity.WorkspaceInvitation, 0)
	for _, invitation := range r.invitations {
		if invitation.WorkspaceID == workspaceID {
			invitation.SetStatus(now)
			items = append(items, invitation)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return repo.PageSlice(items, request), nil
}

func (r *WorkspaceRepo) InvitationsForEmailPage(_ context.Context, email string, request repo.PageRequest) (repo.Page[entity.WorkspaceInvitation], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.load(); err != nil {
		return repo.Page[entity.WorkspaceInvitation]{}, err
	}
	now := time.Now().UTC()
	items := make([]entity.WorkspaceInvitation, 0)
	for _, invitation := range r.invitations {
		if invitation.Email != email || invitation.AcceptedAt != nil || invitation.RevokedAt != nil || !invitation.ExpiresAt.After(now) {
			continue
		}
		for _, workspace := range r.workspaces {
			if workspace.ID == invitation.WorkspaceID {
				invitation.WorkspaceName = workspace.Name
				invitation.WorkspaceSlug = workspace.Slug
				break
			}
		}
		invitation.SetStatus(now)
		items = append(items, invitation)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return repo.PageSlice(items, request), nil
}

func (r *WorkspaceRepo) RevokeInvitation(_ context.Context, workspaceID, invitationID, revokedBy string, revokedAt time.Time) (entity.WorkspaceInvitation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.load(); err != nil {
		return entity.WorkspaceInvitation{}, err
	}
	for index := range r.invitations {
		invitation := &r.invitations[index]
		if invitation.WorkspaceID != workspaceID || invitation.ID != invitationID {
			continue
		}
		invitation.SetStatus(revokedAt)
		if invitation.Status != "pending" {
			return entity.WorkspaceInvitation{}, repo.Conflict("invitation is not pending")
		}
		invitation.RevokedAt = &revokedAt
		invitation.RevokedBy = revokedBy
		invitation.SetStatus(revokedAt)
		if err := r.persist(); err != nil {
			return entity.WorkspaceInvitation{}, err
		}
		return *invitation, nil
	}
	return entity.WorkspaceInvitation{}, repo.NotFound("invitation")
}

func (r *WorkspaceRepo) ClaimInvitation(_ context.Context, expected entity.WorkspaceInvitation, user entity.User, acceptedAt time.Time) (entity.WorkspaceInvitation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.load(); err != nil {
		return entity.WorkspaceInvitation{}, err
	}
	for index := range r.invitations {
		invitation := &r.invitations[index]
		if invitation.ID != expected.ID || invitation.Email != expected.Email {
			continue
		}
		invitation.SetStatus(acceptedAt)
		if invitation.Status != "pending" {
			return entity.WorkspaceInvitation{}, repo.Conflict("invitation is not pending")
		}
		for _, membership := range r.memberships {
			if membership.WorkspaceID == invitation.WorkspaceID && membership.UserID == user.ID {
				return entity.WorkspaceInvitation{}, repo.Conflict("membership already exists")
			}
		}
		r.memberships = append(r.memberships, entity.Membership{WorkspaceID: invitation.WorkspaceID, UserID: user.ID, Role: invitation.Role, CreatedAt: acceptedAt})
		invitation.AcceptedAt = &acceptedAt
		invitation.AcceptedBy = user.ID
		invitation.SetStatus(acceptedAt)
		if err := r.persist(); err != nil {
			return entity.WorkspaceInvitation{}, err
		}
		return *invitation, nil
	}
	return entity.WorkspaceInvitation{}, repo.NotFound("invitation")
}

func (r *WorkspaceRepo) DeleteWorkspace(_ context.Context, workspaceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
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
		return repo.NotFound("workspace")
	}
	r.workspaces = workspaces
	memberships := r.memberships[:0]
	for _, item := range r.memberships {
		if item.WorkspaceID != workspaceID {
			memberships = append(memberships, item)
		}
	}
	r.memberships = memberships
	invitations := r.invitations[:0]
	for _, item := range r.invitations {
		if item.WorkspaceID != workspaceID {
			invitations = append(invitations, item)
		}
	}
	r.invitations = invitations
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
	return r.persist()
}

func (r *WorkspaceRepo) CurrentPolicy(_ context.Context, workspaceID string) (entity.Policy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.load(); err != nil {
		return err
	}
	r.policies = append(r.policies, policy)
	return r.persist()
}

func (r *WorkspaceRepo) CurrentManifest(_ context.Context, workspaceID string) (entity.TeamManifest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
		return entity.TeamManifest{WorkspaceID: workspaceID, Revision: 0, Document: map[string]any{"version": 1, "packages": []any{}}}, nil
	}
	return current, nil
}

func (r *WorkspaceRepo) SaveManifest(_ context.Context, manifest entity.TeamManifest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.load(); err != nil {
		return err
	}
	r.manifests = append(r.manifests, manifest)
	return r.persist()
}
