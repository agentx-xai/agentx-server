package job

import (
	"agentx/server/internal/repo"
	"context"
	"log/slog"
	"time"
)

type AuditRetentionWorker struct {
	Repository    repo.AuditRetentionRepository
	RetentionDays int
	Batch         int
	MaxBatches    int
	Logger        *slog.Logger
	Now           func() time.Time
}

func (w AuditRetentionWorker) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := w.RunOnce(ctx); err != nil {
			w.logger().ErrorContext(ctx, "audit retention cycle failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w AuditRetentionWorker) RunOnce(ctx context.Context) (int64, error) {
	if w.Repository == nil || w.RetentionDays <= 0 {
		return 0, nil
	}
	now := time.Now().UTC()
	if w.Now != nil {
		now = w.Now().UTC()
	}
	batch := w.Batch
	if batch <= 0 {
		batch = 1000
	}
	maxBatches := w.MaxBatches
	if maxBatches <= 0 {
		maxBatches = 100
	}
	var total int64
	for range maxBatches {
		deleted, err := w.Repository.PruneAuditEvents(ctx, now.AddDate(0, 0, -w.RetentionDays), batch)
		if err != nil {
			return total, err
		}
		total += deleted
		if deleted < int64(batch) {
			break
		}
	}
	if total > 0 {
		w.logger().InfoContext(ctx, "expired audit events pruned", "count", total, "retention_days", w.RetentionDays)
	}
	return total, nil
}

func (w AuditRetentionWorker) logger() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}
