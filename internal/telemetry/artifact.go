package telemetry

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"context"
	"io"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type ArtifactStore struct {
	store repo.ArtifactStore
}

func TraceArtifactStore(store repo.ArtifactStore) *ArtifactStore {
	return &ArtifactStore{store: store}
}

func (s *ArtifactStore) Put(ctx context.Context, name string, source io.Reader) (release entity.Release, err error) {
	ctx, span := otel.Tracer("agentx/server/artifact").Start(ctx, "artifact.put")
	defer func() { finish(span, err) }()
	return s.store.Put(ctx, name, source)
}

func (s *ArtifactStore) Open(ctx context.Context, digest string) (reader io.ReadCloser, err error) {
	ctx, span := otel.Tracer("agentx/server/artifact").Start(ctx, "artifact.open")
	defer func() { finish(span, err) }()
	return s.store.Open(ctx, digest)
}

func (s *ArtifactStore) Delete(ctx context.Context, digest string) (err error) {
	ctx, span := otel.Tracer("agentx/server/artifact").Start(ctx, "artifact.delete")
	defer func() { finish(span, err) }()
	return s.store.Delete(ctx, digest)
}

func (s *ArtifactStore) Stat(ctx context.Context, digest string) (release entity.Release, err error) {
	ctx, span := otel.Tracer("agentx/server/artifact").Start(ctx, "artifact.stat")
	defer func() { finish(span, err) }()
	return s.store.Stat(ctx, digest)
}

func (s *ArtifactStore) Ready(ctx context.Context) (err error) {
	ctx, span := otel.Tracer("agentx/server/artifact").Start(ctx, "artifact.ready")
	defer func() { finish(span, err) }()
	if readiness, ok := s.store.(repo.ArtifactReadiness); ok {
		return readiness.Ready(ctx)
	}
	return nil
}

func finish(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "artifact operation failed")
	}
	span.End()
}
