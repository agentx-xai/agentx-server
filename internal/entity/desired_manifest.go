package entity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
)

var (
	packageNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	semverPattern      = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	sha256Pattern      = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type desiredManifestDocument struct {
	Version  int              `json:"version"`
	Packages []DesiredPackage `json:"packages"`
}

func ParseDesiredManifest(document map[string]any) ([]DesiredPackage, error) {
	if document == nil {
		return nil, fmt.Errorf("manifest document is required")
	}
	if _, ok := document["version"]; !ok {
		return nil, fmt.Errorf("manifest version is required")
	}
	if _, ok := document["packages"]; !ok {
		return nil, fmt.Errorf("manifest packages are required")
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var parsed desiredManifestDocument
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("manifest must contain only version and package entries: %w", err)
	}
	if parsed.Version != 1 {
		return nil, fmt.Errorf("unsupported manifest version %d", parsed.Version)
	}
	if parsed.Packages == nil {
		return nil, fmt.Errorf("manifest packages must be an array")
	}
	seen := make(map[string]struct{}, len(parsed.Packages))
	for _, item := range parsed.Packages {
		if !packageNamePattern.MatchString(item.Name) {
			return nil, fmt.Errorf("invalid package name %q", item.Name)
		}
		if !semverPattern.MatchString(item.Version) {
			return nil, fmt.Errorf("invalid package version for %q", item.Name)
		}
		if !sha256Pattern.MatchString(item.SHA256) {
			return nil, fmt.Errorf("invalid package sha256 for %q", item.Name)
		}
		if _, ok := seen[item.Name]; ok {
			return nil, fmt.Errorf("duplicate package name %q", item.Name)
		}
		seen[item.Name] = struct{}{}
	}
	sort.Slice(parsed.Packages, func(i, j int) bool {
		return parsed.Packages[i].Name < parsed.Packages[j].Name
	})
	return parsed.Packages, nil
}
