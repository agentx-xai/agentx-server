package job

import (
	"agentx/server/internal/repo"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type Handler func(context.Context, string, map[string]any) error

type Worker struct {
	Outbox      repo.OutboxRepository
	Handler     Handler
	Logger      *slog.Logger
	Metrics     *Metrics
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
		if err := w.RunOnce(ctx); err != nil {
			if w.Metrics != nil {
				w.Metrics.runFailures.Add(1)
			}
			w.logger().ErrorContext(ctx, "outbox worker cycle failed", "error", err)
		}
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
		if w.Metrics != nil {
			w.Metrics.repositoryFailures.Add(1)
		}
		return err
	}
	var cycleErr error
	for _, event := range events {
		var handlerErr error
		if w.Handler != nil {
			handlerErr = w.Handler(ctx, event.Topic, event.Payload)
		}
		if handlerErr == nil {
			if markErr := w.Outbox.MarkProcessed(ctx, event.ID); markErr != nil {
				if w.Metrics != nil {
					w.Metrics.repositoryFailures.Add(1)
				}
				w.logger().ErrorContext(ctx, "marking outbox event processed", "event_id", event.ID, "error", markErr)
				cycleErr = errors.Join(cycleErr, fmt.Errorf("mark outbox event %s processed: %w", event.ID, markErr))
			}
			continue
		}
		if w.Metrics != nil {
			w.Metrics.handlerFailures.Add(1)
		}
		retryAt := time.Now().UTC().Add(backoff(event.Attempts))
		maxAttempts := w.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = 8
		}
		if event.Attempts >= maxAttempts {
			if markErr := w.Outbox.MarkDeadLetter(ctx, event.ID, handlerErr.Error()); markErr != nil {
				if w.Metrics != nil {
					w.Metrics.repositoryFailures.Add(1)
				}
				w.logger().ErrorContext(ctx, "dead-lettering outbox event", "event_id", event.ID, "error", markErr)
				cycleErr = errors.Join(cycleErr, fmt.Errorf("dead-letter outbox event %s: %w", event.ID, markErr))
				continue
			}
			if w.Metrics != nil {
				w.Metrics.deadLetters.Add(1)
			}
			w.logger().ErrorContext(ctx, "outbox event moved to dead letter", "event_id", event.ID, "topic", event.Topic, "error", handlerErr)
			continue
		}
		if markErr := w.Outbox.MarkFailed(ctx, event.ID, retryAt); markErr != nil {
			if w.Metrics != nil {
				w.Metrics.repositoryFailures.Add(1)
			}
			w.logger().ErrorContext(ctx, "rescheduling outbox event", "event_id", event.ID, "error", markErr)
			cycleErr = errors.Join(cycleErr, fmt.Errorf("reschedule outbox event %s: %w", event.ID, markErr))
			continue
		}
		if w.Metrics != nil {
			w.Metrics.retries.Add(1)
		}
		w.logger().WarnContext(ctx, "outbox handler failed", "event_id", event.ID, "topic", event.Topic, "error", handlerErr, "retry_at", retryAt)
	}
	return cycleErr
}

func (w Worker) logger() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
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
