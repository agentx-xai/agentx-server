package repo

import (
	"agentx/server/internal/entity"
	"context"
	"io"
)

type ArtifactStore interface {
	Put(context.Context, string, io.Reader) (entity.Release, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
	Stat(context.Context, string) (entity.Release, error)
}

type ArtifactReadiness interface {
	Ready(context.Context) error
}

type PackageRepository interface {
	List(context.Context) ([]entity.Release, error)
	Save(context.Context, entity.Release) error
}
type WorkspacePackageRepository interface {
	ListForWorkspace(context.Context, string) ([]entity.Release, error)
	SaveForWorkspace(context.Context, string, entity.Release) error
}
type WorkspacePackagePageRepository interface {
	ListPageForWorkspace(context.Context, string, PageRequest) (Page[entity.Release], error)
}
type PackagePageRepository interface {
	ListPage(context.Context, PageRequest) (Page[entity.Release], error)
}
type WorkspaceArtifactRepository interface {
	HasArtifactForWorkspace(context.Context, string, string) (bool, error)
	FindReleaseByDigestForWorkspace(context.Context, string, string) (entity.Release, error)
	FindReleaseForWorkspace(context.Context, string, string, string) (entity.Release, error)
	ApproveReleaseForWorkspace(context.Context, string, string, string) (entity.Release, error)
}
type ArtifactLifecycleRepository interface {
	DigestsForWorkspace(context.Context, string) ([]string, error)
	RemoveWorkspaceReleases(context.Context, string) error
	ArtifactReferenced(context.Context, string) (bool, error)
	WithArtifactReferenceLock(context.Context, string, func(context.Context) error) error
}
type IdempotencyRepository interface {
	LookupIdempotency(context.Context, string, string) (entity.IdempotencyRecord, bool, error)
	StoreIdempotency(context.Context, string, string, entity.IdempotencyRecord) error
}

type AtomicIdempotencyRepository interface {
	SaveForWorkspaceIdempotent(context.Context, string, string, entity.IdempotencyRecord, entity.AuditEvent, entity.OutboxEvent) (entity.Release, bool, error)
}

type SignatureVerifier interface {
	Verify(context.Context, []byte, string) error
}
