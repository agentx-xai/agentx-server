package job

import (
	"agentx/server/internal/repo"
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
)

type ArtifactCleanupHandler struct {
	References repo.ArtifactLifecycleRepository
	Store      repo.ArtifactStore
	Logger     *slog.Logger
}

func (h ArtifactCleanupHandler) Handle(ctx context.Context, payload map[string]any) error {
	digest, ok := payload["digest"].(string)
	if !ok || !validArtifactDigest(digest) {
		return fmt.Errorf("artifact cleanup payload has invalid digest")
	}
	if h.References == nil || h.Store == nil {
		return fmt.Errorf("artifact cleanup dependencies are unavailable")
	}
	return h.References.WithArtifactReferenceLock(ctx, digest, func(lockCtx context.Context) error {
		referenced, err := h.References.ArtifactReferenced(lockCtx, digest)
		if err != nil {
			return err
		}
		logger := h.Logger
		if logger == nil {
			logger = slog.Default()
		}
		if referenced {
			logger.InfoContext(lockCtx, "artifact cleanup retained referenced digest", "digest", digest)
			return nil
		}
		if err = h.Store.Delete(lockCtx, digest); err != nil {
			return err
		}
		logger.InfoContext(lockCtx, "artifact garbage collected", "digest", digest)
		return nil
	})
}

func validArtifactDigest(digest string) bool {
	if len(digest) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(digest)
	return err == nil && len(decoded) == 32
}
