package entity

import "time"

type AuditEvent struct {
	Action       string         `json:"action"`
	ActorID      string         `json:"actor_id,omitempty"`
	DeviceID     string         `json:"device_id,omitempty"`
	ResourceType string         `json:"resource_type,omitempty"`
	ResourceID   string         `json:"resource_id,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	RequestID    string         `json:"request_id,omitempty"`
	At           time.Time      `json:"at"`
}
