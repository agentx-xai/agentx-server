package entity

import "time"

type Package struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}
type Release struct {
	Name            string    `json:"name"`
	Version         string    `json:"version"`
	SHA256          string    `json:"sha256"`
	Size            int64     `json:"size"`
	CreatedAt       time.Time `json:"created_at"`
	Signature       string    `json:"signature,omitempty"`
	SignatureStatus string    `json:"signature_status,omitempty"`
	Status          string    `json:"status,omitempty"`
}
