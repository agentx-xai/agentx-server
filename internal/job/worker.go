package job

import (
	"agentx/server/internal/repo"
	"context"
	"log/slog"
	"time"
)

type Handler func(context.Context, string, map[string]any) error

type Worker struct {
	Outbox      repo.OutboxRepository
	Handler     Handler
	Logger      *slog.Logger
	Batch       int
	MaxAttempts int
}

func (w Worker) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		_ = w.RunOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w Worker) RunOnce(ctx context.Context) error {
	if w.Outbox == nil {
		return nil
	}
	batch := w.Batch
	if batch <= 0 {
		batch = 50
	}
	events, err := w.Outbox.Claim(ctx, batch)
	if err != nil {
		return err
	}
	for _, event := range events {
		err := error(nil)
		if w.Handler != nil {
			err = w.Handler(ctx, event.Topic, event.Payload)
		}
		if err == nil {
			if markErr := w.Outbox.MarkProcessed(ctx, event.ID); markErr != nil && w.Logger != nil {
				w.Logger.Error("marking outbox event processed", "event_id", event.ID, "error", markErr)
			}
			continue
		}
		retryAt := time.Now().UTC().Add(backoff(event.Attempts))
		maxAttempts := w.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = 8
		}
		if event.Attempts >= maxAttempts {
			if markErr := w.Outbox.MarkDeadLetter(ctx, event.ID, err.Error()); markErr != nil && w.Logger != nil {
				w.Logger.Error("dead-lettering outbox event", "event_id", event.ID, "error", markErr)
			}
			if w.Logger != nil {
				w.Logger.Error("outbox event moved to dead letter", "event_id", event.ID, "topic", event.Topic, "error", err)
			}
			continue
		}
		if markErr := w.Outbox.MarkFailed(ctx, event.ID, retryAt); markErr != nil && w.Logger != nil {
			w.Logger.Error("rescheduling outbox event", "event_id", event.ID, "error", markErr)
		}
		if w.Logger != nil {
			w.Logger.Warn("outbox handler failed", "event_id", event.ID, "topic", event.Topic, "error", err, "retry_at", retryAt)
		}
	}
	return nil
}

func backoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 6 {
		attempts = 6
	}
	return time.Duration(1<<attempts) * time.Second
}
