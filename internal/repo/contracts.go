package repo

import (
	"agentx/server/internal/entity"
	"context"
	"time"
)

type UnitOfWork interface {
	WithinTransaction(context.Context, func(context.Context) error) error
}

type PageRequest struct {
	Limit  int
	Offset int
}

type Page[T any] struct {
	Items []T
	Total int
}

func PageSlice[T any](items []T, request PageRequest) Page[T] {
	total := len(items)
	if request.Offset >= total {
		return Page[T]{Items: []T{}, Total: total}
	}
	end := request.Offset + request.Limit
	if end > total {
		end = total
	}
	return Page[T]{Items: items[request.Offset:end], Total: total}
}

type DeviceRepository interface {
	List(context.Context) ([]entity.Device, error)
	Save(context.Context, entity.Device) error
}
type WorkspaceDeviceRepository interface {
	ListForWorkspace(context.Context, string) ([]entity.Device, error)
	SaveForWorkspace(context.Context, string, entity.Device) error
}
type WorkspaceDevicePageRepository interface {
	ListPageForWorkspace(context.Context, string, PageRequest) (Page[entity.Device], error)
}
type DevicePageRepository interface {
	ListPage(context.Context, PageRequest) (Page[entity.Device], error)
}
type WorkspaceDeviceHeartbeat interface {
	HeartbeatForWorkspace(context.Context, string, string, entity.Device) error
}
type AtomicDeviceIdempotencyRepository interface {
	SaveForWorkspaceIdempotent(context.Context, string, string, entity.DeviceIdempotencyRecord, entity.AuditEvent, entity.OutboxEvent) (entity.Device, bool, error)
}
type AuditRepository interface {
	List(context.Context) ([]entity.AuditEvent, error)
	Append(context.Context, entity.AuditEvent) error
}
type WorkspaceAuditRepository interface {
	ListForWorkspace(context.Context, string) ([]entity.AuditEvent, error)
}
type WorkspaceAuditPageRepository interface {
	ListPageForWorkspace(context.Context, string, PageRequest) (Page[entity.AuditEvent], error)
}
type AuditPageRepository interface {
	ListPage(context.Context, PageRequest) (Page[entity.AuditEvent], error)
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

type WorkspacePageRepository interface {
	ListPage(context.Context, string, PageRequest) (Page[entity.Workspace], error)
	MembersPage(context.Context, string, PageRequest) (Page[entity.Membership], error)
}

type WorkspaceInvitationRepository interface {
	CreateInvitation(context.Context, entity.WorkspaceInvitation) error
	Invitation(context.Context, string, string) (entity.WorkspaceInvitation, error)
	InvitationForEmail(context.Context, string, string) (entity.WorkspaceInvitation, error)
	InvitationsForWorkspacePage(context.Context, string, PageRequest) (Page[entity.WorkspaceInvitation], error)
	InvitationsForEmailPage(context.Context, string, PageRequest) (Page[entity.WorkspaceInvitation], error)
	RevokeInvitation(context.Context, string, string, string, time.Time) (entity.WorkspaceInvitation, error)
	ClaimInvitation(context.Context, entity.WorkspaceInvitation, entity.User, time.Time) (entity.WorkspaceInvitation, error)
}

type AccountRepository interface {
	ExportAccount(context.Context, string, string) (entity.AccountExport, error)
	DeleteAccount(context.Context, entity.AccountDeletion) error
}

type LegalHoldFilter struct {
	Status     string
	TargetType string
	TargetID   string
}

type LegalHoldRepository interface {
	CreateLegalHold(context.Context, entity.LegalHold, entity.AuditEvent, entity.OutboxEvent) error
	LegalHold(context.Context, string) (entity.LegalHold, error)
	ListLegalHoldsPage(context.Context, LegalHoldFilter, PageRequest) (Page[entity.LegalHold], error)
	ReleaseLegalHold(context.Context, string, string, string, time.Time, entity.AuditEvent, entity.OutboxEvent) (entity.LegalHold, error)
}

type AuditRetentionRepository interface {
	PruneAuditEvents(context.Context, time.Time, int) (int64, error)
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
