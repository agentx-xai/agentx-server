package file

import (
	"agentx/server/internal/entity"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type OutboxRepo struct {
	path string
}

func NewOutboxRepo(dir string) *OutboxRepo {
	return &OutboxRepo{path: filepath.Join(dir, "outbox.json")}
}

func (r *OutboxRepo) Enqueue(_ context.Context, event entity.OutboxEvent) error {
	dataMu.Lock()
	defer dataMu.Unlock()
	items, err := r.load()
	if err != nil {
		return err
	}
	items = append(items, event)
	return r.save(items)
}

func (r *OutboxRepo) Claim(_ context.Context, limit int) ([]entity.OutboxEvent, error) {
	dataMu.Lock()
	defer dataMu.Unlock()
	items, err := r.load()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var out []entity.OutboxEvent
	for i := range items {
		if items[i].ProcessedAt == nil && items[i].DeadLetteredAt == nil && !items[i].AvailableAt.After(now) && len(out) < limit {
			items[i].Attempts++
			out = append(out, items[i])
		}
	}
	if len(out) > 0 {
		if err := r.save(items); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *OutboxRepo) MarkProcessed(_ context.Context, id string) error {
	dataMu.Lock()
	defer dataMu.Unlock()
	items, err := r.load()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for i := range items {
		if items[i].ID == id {
			items[i].ProcessedAt = &now
			return r.save(items)
		}
	}
	return errors.New("outbox event not found")
}

func (r *OutboxRepo) MarkFailed(_ context.Context, id string, retryAt time.Time) error {
	dataMu.Lock()
	defer dataMu.Unlock()
	items, err := r.load()
	if err != nil {
		return err
	}
	for i := range items {
		if items[i].ID == id {
			items[i].AvailableAt = retryAt
			return r.save(items)
		}
	}
	return errors.New("outbox event not found")
}

func (r *OutboxRepo) MarkDeadLetter(_ context.Context, id, reason string) error {
	dataMu.Lock()
	defer dataMu.Unlock()
	items, err := r.load()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for i := range items {
		if items[i].ID == id {
			items[i].DeadLetteredAt = &now
			items[i].LastError = reason
			return r.save(items)
		}
	}
	return errors.New("outbox event not found")
}

func (r *OutboxRepo) load() ([]entity.OutboxEvent, error) {
	b, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return []entity.OutboxEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	var items []entity.OutboxEvent
	if err := json.Unmarshal(b, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *OutboxRepo) save(items []entity.OutboxEvent) error {
	sort.SliceStable(items, func(i, j int) bool { return items[i].AvailableAt.Before(items[j].AvailableAt) })
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0750); err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0640); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}
