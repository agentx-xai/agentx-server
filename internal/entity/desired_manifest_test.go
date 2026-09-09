package entity

import "testing"

const testDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestParseDesiredManifestValidatesAndSorts(t *testing.T) {
	packages, err := ParseDesiredManifest(map[string]any{
		"version": 1,
		"packages": []any{
			map[string]any{"name": "zeta", "version": "1.2.3-beta.1+build", "sha256": testDigest},
			map[string]any{"name": "alpha", "version": "0.1.0", "sha256": testDigest},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 2 || packages[0].Name != "alpha" || packages[1].Name != "zeta" {
		t.Fatalf("packages are not deterministic: %+v", packages)
	}
}

func TestParseDesiredManifestRejectsMalformedDocuments(t *testing.T) {
	tests := []map[string]any{
		{"packages": []any{}},
		{"version": 1},
		{"version": 2, "packages": []any{}},
		{"version": 1, "packages": nil},
		{"version": 1, "packages": []any{map[string]any{"name": "demo", "version": "latest", "sha256": testDigest}}},
		{"version": 1, "packages": []any{map[string]any{"name": "demo", "version": "1.0.0", "sha256": "ABC"}}},
		{"version": 1, "packages": []any{map[string]any{"name": "demo", "version": "1.0.0", "sha256": testDigest, "extra": true}}},
		{"version": 1, "packages": []any{
			map[string]any{"name": "demo", "version": "1.0.0", "sha256": testDigest},
			map[string]any{"name": "demo", "version": "2.0.0", "sha256": testDigest},
		}},
		{"version": 1, "packages": []any{}, "skills": []any{}},
	}
	for index, document := range tests {
		if _, err := ParseDesiredManifest(document); err == nil {
			t.Fatalf("case %d unexpectedly accepted: %#v", index, document)
		}
	}
}

func TestParseDesiredManifestAcceptsExplicitEmptyState(t *testing.T) {
	packages, err := ParseDesiredManifest(map[string]any{"version": 1, "packages": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	if packages == nil || len(packages) != 0 {
		t.Fatalf("expected an explicit empty package list, got %#v", packages)
	}
}
