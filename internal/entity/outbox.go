package entity

import "time"

type OutboxEvent struct {
	ID             string         `json:"id"`
	Topic          string         `json:"topic"`
	Payload        map[string]any `json:"payload"`
	Attempts       int            `json:"attempts"`
	AvailableAt    time.Time      `json:"available_at"`
	ProcessedAt    *time.Time     `json:"processed_at,omitempty"`
	DeadLetteredAt *time.Time     `json:"dead_lettered_at,omitempty"`
	LastError      string         `json:"last_error,omitempty"`
}
