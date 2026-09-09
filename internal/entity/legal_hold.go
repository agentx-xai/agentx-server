package entity

import "time"

const (
	LegalHoldTargetAccount   = "account"
	LegalHoldTargetWorkspace = "workspace"
)

type LegalHold struct {
	ID            string     `json:"id"`
	TargetType    string     `json:"target_type"`
	TargetID      string     `json:"target_id"`
	Reason        string     `json:"reason"`
	CreatedBy     string     `json:"created_by,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	ReleasedBy    string     `json:"released_by,omitempty"`
	ReleasedAt    *time.Time `json:"released_at,omitempty"`
	ReleaseReason string     `json:"release_reason,omitempty"`
}
