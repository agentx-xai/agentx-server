package job

import (
	"context"
	"errors"
	"testing"
	"time"
)

type retentionRepository struct {
	before time.Time
	limit  int
	count  int64
	calls  int
	err    error
}

func (r *retentionRepository) PruneAuditEvents(_ context.Context, before time.Time, limit int) (int64, error) {
	r.before, r.limit = before, limit
	r.calls++
	if r.calls > 1 {
		return 0, r.err
	}
	return r.count, r.err
}

func TestAuditRetentionWorkerUsesConfiguredCutoffAndBatch(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.FixedZone("test", 8*60*60))
	repository := &retentionRepository{count: 7}
	worker := AuditRetentionWorker{Repository: repository, RetentionDays: 365, Batch: 250, Now: func() time.Time { return now }}
	count, err := worker.RunOnce(context.Background())
	if err != nil || count != 7 {
		t.Fatalf("unexpected retention result: %d %v", count, err)
	}
	want := now.UTC().AddDate(0, 0, -365)
	if !repository.before.Equal(want) || repository.limit != 250 {
		t.Fatalf("unexpected cutoff or batch: %s %d", repository.before, repository.limit)
	}
}

func TestAuditRetentionWorkerCanBeDisabledAndReturnsErrors(t *testing.T) {
	repository := &retentionRepository{err: errors.New("database unavailable")}
	worker := AuditRetentionWorker{Repository: repository}
	if count, err := worker.RunOnce(context.Background()); err != nil || count != 0 || !repository.before.IsZero() {
		t.Fatalf("disabled worker should not call repository: %d %v", count, err)
	}
	worker.RetentionDays = 30
	if _, err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("expected repository failure")
	}
}
