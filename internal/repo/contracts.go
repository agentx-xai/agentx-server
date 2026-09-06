package repo

import (
	"agentx/server/internal/entity"
	"context"
	"time"
)

type DeviceRepository interface {
	List(context.Context) ([]entity.Device, error)
	Save(context.Context, entity.Device) error
}
type WorkspaceDeviceRepository interface {
	ListForWorkspace(context.Context, string) ([]entity.Device, error)
	SaveForWorkspace(context.Context, string, entity.Device) error
}
type WorkspaceDeviceHeartbeat interface {
	HeartbeatForWorkspace(context.Context, string, string, entity.Device) error
}
type AuditRepository interface {
	List(context.Context) ([]entity.AuditEvent, error)
	Append(context.Context, entity.AuditEvent) error
}
type WorkspaceAuditRepository interface {
	ListForWorkspace(context.Context, string) ([]entity.AuditEvent, error)
}
type WorkspaceAuditWriter interface {
	AppendForWorkspace(context.Context, string, entity.AuditEvent) error
}

type WorkspaceRepository interface {
	List(context.Context, string) ([]entity.Workspace, error)
	Create(context.Context, entity.Workspace, entity.Membership) error
	Membership(context.Context, string, string) (entity.Membership, error)
	Members(context.Context, string) ([]entity.Membership, error)
	AddMember(context.Context, entity.Membership) error
	UpdateMemberRole(context.Context, string, string, entity.Role) error
	RemoveMember(context.Context, string, string) error
	DeleteWorkspace(context.Context, string) error
}

type PolicyRepository interface {
	CurrentPolicy(context.Context, string) (entity.Policy, error)
	SavePolicy(context.Context, entity.Policy) error
}

type ManifestRepository interface {
	CurrentManifest(context.Context, string) (entity.TeamManifest, error)
	SaveManifest(context.Context, entity.TeamManifest) error
}

type OutboxRepository interface {
	Enqueue(context.Context, entity.OutboxEvent) error
	Claim(context.Context, int) ([]entity.OutboxEvent, error)
	MarkProcessed(context.Context, string) error
	MarkFailed(context.Context, string, time.Time) error
	MarkDeadLetter(context.Context, string, string) error
}
