package http

import (
	"agentx/server/internal/repo/file"
	"agentx/server/internal/usecase/device"
	"agentx/server/internal/usecase/registry"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOIDCClientConfigIsPublicAndContainsNoSecret(t *testing.T) {
	dir := t.TempDir()
	router := NewRouter(
		device.New(file.NewDeviceRepo(dir), file.NewAuditRepo(dir)),
		registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir)),
		RouterOptions{
			APIToken: "integration-token",
			OIDCClient: OIDCClientConfig{
				Issuer:   "https://id.example.com",
				ClientID: "agentx-cli",
				Audience: "agentx-api",
				Scope:    "openid profile offline_access",
			},
		},
	)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/auth/config", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["issuer"] != "https://id.example.com" || body["client_id"] != "agentx-cli" || body["audience"] != "agentx-api" {
		t.Fatalf("unexpected config: %#v", body)
	}
	if _, found := body["client_secret"]; found {
		t.Fatal("public OIDC config exposed a client secret")
	}
}

func TestOIDCClientConfigFailsClosedWhenUnavailable(t *testing.T) {
	dir := t.TempDir()
	router := NewRouter(
		device.New(file.NewDeviceRepo(dir), file.NewAuditRepo(dir)),
		registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir)),
	)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/auth/config", nil))
	if response.Code != http.StatusNotFound || response.Header().Get("X-Request-ID") == "" {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}
