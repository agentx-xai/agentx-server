package entity

import "time"

// TeamManifest is the workspace source of truth for the desired agent environment.
type TeamManifest struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	Revision    int64          `json:"revision"`
	Document    map[string]any `json:"document"`
	CreatedAt   time.Time      `json:"created_at"`
}
