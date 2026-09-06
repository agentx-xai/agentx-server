package job

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo/file"
	"context"
	"errors"
	"path/filepath"
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
	worker := Worker{Outbox: outbox, MaxAttempts: 1, Handler: func(context.Context, string, map[string]any) error { return errors.New("permanent failure") }}
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
}
