package entity

import "time"

const AccountExportSchemaVersion = 1

type AccountMembership struct {
	Workspace Workspace `json:"workspace"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type AccountAuditEvent struct {
	WorkspaceID  string    `json:"workspace_id,omitempty"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type,omitempty"`
	ResourceID   string    `json:"resource_id,omitempty"`
	RequestID    string    `json:"request_id,omitempty"`
	At           time.Time `json:"at"`
}

type AccountExport struct {
	SchemaVersion int                   `json:"schema_version"`
	ExportedAt    time.Time             `json:"exported_at"`
	User          User                  `json:"user"`
	Memberships   []AccountMembership   `json:"memberships"`
	Invitations   []WorkspaceInvitation `json:"invitations"`
	AuditEvents   []AccountAuditEvent   `json:"audit_events"`
}

type AccountDeletion struct {
	UserID     string
	Email      string
	DeletionID string
	RequestID  string
	At         time.Time
}
