package entity

import "time"

type Device struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Agent             string            `json:"agent"`
	Status            string            `json:"status"`
	UpdatedAt         time.Time         `json:"updated_at"`
	InstalledPackages map[string]string `json:"installed_packages,omitempty"`
}
