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
type WorkspaceArtifactRepository interface {
	HasArtifactForWorkspace(context.Context, string, string) (bool, error)
	FindReleaseByDigestForWorkspace(context.Context, string, string) (entity.Release, error)
	FindReleaseForWorkspace(context.Context, string, string, string) (entity.Release, error)
	ApproveReleaseForWorkspace(context.Context, string, string, string) (entity.Release, error)
}
type IdempotencyRepository interface {
	LookupIdempotency(context.Context, string, string) (entity.Release, bool, error)
	StoreIdempotency(context.Context, string, string, entity.Release) error
}

// IdempotencyFingerprintRepository lets callers detect accidental reuse of a
// key for a different request. Implementations should persist the fingerprint
// atomically with the response record.
type IdempotencyFingerprintRepository interface {
	LookupIdempotencyFingerprint(context.Context, string, string) (entity.Release, bool, string, error)
	StoreIdempotencyFingerprint(context.Context, string, string, string, entity.Release) error
}

type SignatureVerifier interface {
	Verify(context.Context, []byte, string) error
}
