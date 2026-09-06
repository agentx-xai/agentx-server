package http

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo/file"
	"agentx/server/internal/usecase/device"
	"agentx/server/internal/usecase/registry"
	"agentx/server/internal/usecase/workspace"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorkspacePackageCursorPagination(t *testing.T) {
	dir := t.TempDir()
	workspaceService := workspace.New(file.NewWorkspaceRepo(dir))
	registryService := registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir))
	router := NewRouter(device.New(file.NewDeviceRepo(dir), file.NewAuditRepo(dir)), registryService, RouterOptions{Workspace: workspaceService})
	create := httptest.NewRecorder()
	router.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/v1/workspaces", bytes.NewBufferString(`{"name":"Platform","slug":"platform"}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create workspace: %d", create.Code)
	}
	var w entity.Workspace
	if err := json.Unmarshal(create.Body.Bytes(), &w); err != nil {
		t.Fatal(err)
	}
	packages := file.NewPackageRepo(dir)
	for i, name := range []string{"a", "b"} {
		if err := packages.SaveForWorkspace(context.Background(), w.ID, entity.Release{Name: name, Version: "1.0.0", SHA256: name}); err != nil {
			t.Fatal(err)
		}
		_ = i
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+w.ID+"/packages?limit=1", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("list packages: %d %s", response.Code, response.Body.String())
	}
	var page struct {
		Items      []entity.Release `json:"items"`
		NextCursor string           `json:"next_cursor"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("unexpected first page: %+v", page)
	}
	defaultResponse := httptest.NewRecorder()
	router.ServeHTTP(defaultResponse, httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+w.ID+"/packages", nil))
	var defaultPage struct {
		Items []entity.Release `json:"items"`
	}
	if err := json.Unmarshal(defaultResponse.Body.Bytes(), &defaultPage); err != nil || len(defaultPage.Items) != 2 {
		t.Fatalf("expected default paginated response: %s %v", defaultResponse.Body.String(), err)
	}
}
