package job

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo/file"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkerProcessesAndRetriesOutboxEvents(t *testing.T) {
	dir := t.TempDir()
	outbox := file.NewOutboxRepo(dir)
	if err := outbox.Enqueue(context.Background(), entity.OutboxEvent{ID: "event-1", Topic: "release.published", Payload: map[string]any{"name": "demo"}, AvailableAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	called := 0
	worker := Worker{Outbox: outbox, Handler: func(_ context.Context, topic string, payload map[string]any) error {
		called++
		if topic != "release.published" || payload["name"] != "demo" {
			t.Fatalf("unexpected event: %s %+v", topic, payload)
		}
		return nil
	}}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("handler called %d times", called)
	}
	items, err := outbox.Claim(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("processed event was claimable: %+v", items)
	}
	if _, err := filepath.Abs(dir); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerDeadLettersAfterMaximumAttempts(t *testing.T) {
	dir := t.TempDir()
	outbox := file.NewOutboxRepo(dir)
	if err := outbox.Enqueue(context.Background(), entity.OutboxEvent{ID: "dead-1", Topic: "broken", Payload: map[string]any{}, AvailableAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	metrics := &Metrics{}
	worker := Worker{Outbox: outbox, Metrics: metrics, MaxAttempts: 1, Handler: func(context.Context, string, map[string]any) error { return errors.New("permanent failure") }}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, err := outbox.Claim(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("dead-letter event was claimable: %+v", items)
	}
	snapshot := metrics.Snapshot()
	if snapshot.HandlerFailures != 1 || snapshot.DeadLetters != 1 {
		t.Fatalf("unexpected worker metrics: %+v", snapshot)
	}
}

type failingClaimOutbox struct {
	err    error
	cancel context.CancelFunc
}

func (o failingClaimOutbox) Enqueue(context.Context, entity.OutboxEvent) error { return nil }
func (o failingClaimOutbox) Claim(context.Context, int) ([]entity.OutboxEvent, error) {
	o.cancel()
	return nil, o.err
}
func (o failingClaimOutbox) MarkProcessed(context.Context, string) error          { return nil }
func (o failingClaimOutbox) MarkFailed(context.Context, string, time.Time) error  { return nil }
func (o failingClaimOutbox) MarkDeadLetter(context.Context, string, string) error { return nil }

func TestWorkerRunLogsAndCountsCycleFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var output bytes.Buffer
	metrics := &Metrics{}
	worker := Worker{
		Outbox:  failingClaimOutbox{err: errors.New("database unavailable"), cancel: cancel},
		Logger:  slog.New(slog.NewTextHandler(&output, nil)),
		Metrics: metrics,
	}
	worker.Run(ctx, time.Hour)
	if !strings.Contains(output.String(), "outbox worker cycle failed") || !strings.Contains(output.String(), "database unavailable") {
		t.Fatalf("worker failure was not logged: %s", output.String())
	}
	snapshot := metrics.Snapshot()
	if snapshot.RunFailures != 1 || snapshot.RepositoryFailures != 1 {
		t.Fatalf("unexpected worker metrics: %+v", snapshot)
	}
}
