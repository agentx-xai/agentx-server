package entity

import "time"

type Policy struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	Revision    int64          `json:"revision"`
	Document    map[string]any `json:"document"`
	CreatedAt   time.Time      `json:"created_at"`
}
