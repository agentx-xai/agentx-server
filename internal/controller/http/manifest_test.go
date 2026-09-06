package http

import (
	"agentx/server/internal/repo/file"
	"agentx/server/internal/usecase/device"
	"agentx/server/internal/usecase/manifest"
	"agentx/server/internal/usecase/registry"
	"agentx/server/internal/usecase/workspace"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManifestLifecycle(t *testing.T) {
	dir := t.TempDir()
	workspaces := workspace.New(file.NewWorkspaceRepo(dir))
	router := NewRouter(device.New(file.NewDeviceRepo(dir), file.NewAuditRepo(dir)), registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir)), RouterOptions{Workspace: workspaces, Manifest: manifest.New(file.NewWorkspaceRepo(dir), workspaces)})
	created := httptest.NewRecorder()
	router.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/v1/workspaces", bytes.NewBufferString(`{"name":"Platform","slug":"platform"}`)))
	if created.Code != http.StatusCreated {
		t.Fatalf("create workspace: %d", created.Code)
	}
	var workspace struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &workspace); err != nil {
		t.Fatal(err)
	}
	workspaceID := workspace.ID
	update := httptest.NewRecorder()
	router.ServeHTTP(update, httptest.NewRequest(http.MethodPut, "/v1/workspaces/"+workspaceID+"/manifest", bytes.NewBufferString(`{"document":{"packages":[{"name":"demo","sha256":"abc"}]}}`)))
	if update.Code != http.StatusOK || !bytes.Contains(update.Body.Bytes(), []byte(`"revision":1`)) {
		t.Fatalf("update manifest: %d %s", update.Code, update.Body.String())
	}
}
