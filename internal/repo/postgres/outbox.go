package postgres

import (
	"agentx/server/internal/entity"
	"context"
	"encoding/json"
	"fmt"
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
	_, err = r.exec(ctx, `INSERT INTO outbox(id,topic,payload_json,attempts,available_at) VALUES($1,$2,$3,$4,$5)`, event.ID, event.Topic, raw, event.Attempts, event.AvailableAt)
	return err
}

func (r OutboxRepo) Claim(ctx context.Context, limit int) ([]entity.OutboxEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.Pool.Query(ctx, `
WITH candidates AS (
    SELECT id FROM outbox
    WHERE processed_at IS NULL AND dead_lettered_at IS NULL AND available_at <= clock_timestamp()
    ORDER BY available_at
    FOR UPDATE SKIP LOCKED
    LIMIT $1
)
UPDATE outbox AS event
SET attempts=event.attempts+1, available_at=clock_timestamp()+interval '5 minutes'
FROM candidates
WHERE event.id=candidates.id
RETURNING event.id,event.topic,event.payload_json,event.attempts,event.available_at`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r OutboxRepo) MarkProcessed(ctx context.Context, id string) error {
	result, err := r.Pool.Exec(ctx, `UPDATE outbox SET processed_at=now() WHERE id=$1`, id)
	return outboxUpdateResult(id, result.RowsAffected(), err)
}

func (r OutboxRepo) MarkFailed(ctx context.Context, id string, retryAt time.Time) error {
	result, err := r.Pool.Exec(ctx, `UPDATE outbox SET available_at=$2 WHERE id=$1`, id, retryAt)
	return outboxUpdateResult(id, result.RowsAffected(), err)
}

func (r OutboxRepo) MarkDeadLetter(ctx context.Context, id, reason string) error {
	result, err := r.Pool.Exec(ctx, `UPDATE outbox SET dead_lettered_at=now(),last_error=$2 WHERE id=$1`, id, reason)
	return outboxUpdateResult(id, result.RowsAffected(), err)
}

func outboxUpdateResult(id string, rowsAffected int64, err error) error {
	if err != nil {
		return err
	}
	if rowsAffected != 1 {
		return fmt.Errorf("outbox event %s not found", id)
	}
	return nil
}
