package entity

import "time"

type Role string

const (
	RoleOwner     Role = "owner"
	RoleAdmin     Role = "admin"
	RoleDeveloper Role = "developer"
	RoleViewer    Role = "viewer"
)

type User struct {
	ID            string    `json:"id"`
	Issuer        string    `json:"issuer"`
	Subject       string    `json:"subject"`
	Email         string    `json:"email,omitempty"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
}

type Workspace struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Membership struct {
	WorkspaceID string    `json:"workspace_id"`
	UserID      string    `json:"user_id"`
	Role        Role      `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
}

type WorkspaceInvitation struct {
	ID            string     `json:"id"`
	WorkspaceID   string     `json:"workspace_id"`
	WorkspaceName string     `json:"workspace_name,omitempty"`
	WorkspaceSlug string     `json:"workspace_slug,omitempty"`
	Email         string     `json:"email"`
	Role          Role       `json:"role"`
	CreatedBy     string     `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     time.Time  `json:"expires_at"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	RevokedBy     string     `json:"revoked_by,omitempty"`
	AcceptedAt    *time.Time `json:"accepted_at,omitempty"`
	AcceptedBy    string     `json:"accepted_by,omitempty"`
	Status        string     `json:"status"`
}

func (i *WorkspaceInvitation) SetStatus(now time.Time) {
	switch {
	case i.AcceptedAt != nil:
		i.Status = "accepted"
	case i.RevokedAt != nil:
		i.Status = "revoked"
	case !i.ExpiresAt.After(now):
		i.Status = "expired"
	default:
		i.Status = "pending"
	}
}

func (r Role) Allows(required Role) bool {
	rank := map[Role]int{RoleViewer: 1, RoleDeveloper: 2, RoleAdmin: 3, RoleOwner: 4}
	return rank[r] >= rank[required]
}
