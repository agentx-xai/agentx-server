package postgres

import (
	"agentx/server/internal/entity"
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type OutboxRepo struct{ *Store }

func (r OutboxRepo) Enqueue(ctx context.Context, event entity.OutboxEvent) error {
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.AvailableAt.IsZero() {
		event.AvailableAt = time.Now().UTC()
	}
	raw, err := json.Marshal(event.Payload)
	if err != nil {
		return err
	}
	_, err = r.Pool.Exec(ctx, `INSERT INTO outbox(id,topic,payload_json,attempts,available_at) VALUES($1,$2,$3,$4,$5)`, event.ID, event.Topic, raw, event.Attempts, event.AvailableAt)
	return err
}

func (r OutboxRepo) Claim(ctx context.Context, limit int) ([]entity.OutboxEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id,topic,payload_json,attempts,available_at FROM outbox WHERE processed_at IS NULL AND dead_lettered_at IS NULL AND available_at <= now() ORDER BY available_at FOR UPDATE SKIP LOCKED LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	var out []entity.OutboxEvent
	for rows.Next() {
		var event entity.OutboxEvent
		var raw []byte
		if err := rows.Scan(&event.ID, &event.Topic, &raw, &event.Attempts, &event.AvailableAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &event.Payload); err != nil {
			return nil, err
		}
		event.Attempts++
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, event := range out {
		if _, err := tx.Exec(ctx, `UPDATE outbox SET attempts=attempts+1 WHERE id=$1`, event.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (r OutboxRepo) MarkProcessed(ctx context.Context, id string) error {
	_, err := r.Pool.Exec(ctx, `UPDATE outbox SET processed_at=now() WHERE id=$1`, id)
	return err
}

func (r OutboxRepo) MarkFailed(ctx context.Context, id string, retryAt time.Time) error {
	_, err := r.Pool.Exec(ctx, `UPDATE outbox SET available_at=$2 WHERE id=$1`, id, retryAt)
	return err
}

func (r OutboxRepo) MarkDeadLetter(ctx context.Context, id, reason string) error {
	_, err := r.Pool.Exec(ctx, `UPDATE outbox SET dead_lettered_at=now(),last_error=$2 WHERE id=$1`, id, reason)
	return err
}
