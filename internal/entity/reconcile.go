package entity

import "time"

type DesiredPackage struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	SHA256  string `json:"sha256"`
}

type ReconcileAction struct {
	Package string `json:"package"`
	Kind    string `json:"kind"`
	From    string `json:"from,omitempty"`
	To      string `json:"to"`
}

type ReconcilePlan struct {
	DeviceID  string            `json:"device_id"`
	Workspace string            `json:"workspace_id"`
	Revision  int64             `json:"manifest_revision"`
	Actions   []ReconcileAction `json:"actions"`
	Generated time.Time         `json:"generated_at"`
}
