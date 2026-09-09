package http

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo/file"
	"agentx/server/internal/usecase/device"
	"agentx/server/internal/usecase/registry"
	"agentx/server/internal/usecase/workspace"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRepositoryConflictsAndMissingResourcesArePublicErrors(t *testing.T) {
	dir := t.TempDir()
	workspaceService := workspace.New(file.NewWorkspaceRepo(dir))
	registryService := registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir))
	registryService.SetWorkspace(workspaceService)
	router := NewRouter(device.New(file.NewDeviceRepo(dir), file.NewAuditRepo(dir)), registryService, RouterOptions{Workspace: workspaceService})

	createWorkspace := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/v1/workspaces", bytes.NewBufferString(`{"name":"Platform","slug":"platform"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	created := createWorkspace()
	if created.Code != http.StatusCreated {
		t.Fatalf("create workspace: %d %s", created.Code, created.Body.String())
	}
	duplicateWorkspace := createWorkspace()
	assertPublicError(t, duplicateWorkspace, http.StatusBadRequest, "WORKSPACE_INVALID", "workspace slug already exists")

	var current entity.Workspace
	if err := json.Unmarshal(created.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	publish := func(key string) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if err := writer.WriteField("version", "1.0.0"); err != nil {
			t.Fatal(err)
		}
		part, err := writer.CreateFormFile("artifact", "demo.tar")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write([]byte("artifact")); err != nil {
			t.Fatal(err)
		}
		if err = writer.Close(); err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/v1/workspaces/"+current.ID+"/packages/demo/releases", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		request.Header.Set("Idempotency-Key", key)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	if response := publish("release-1"); response.Code != http.StatusCreated {
		t.Fatalf("publish release: %d %s", response.Code, response.Body.String())
	}
	assertPublicError(t, publish("release-2"), http.StatusBadRequest, "PUBLISH_FAILED", "release version already exists")
	publishedApproval := httptest.NewRecorder()
	router.ServeHTTP(publishedApproval, httptest.NewRequest(http.MethodPost, "/v1/workspaces/"+current.ID+"/packages/demo/1.0.0/approve", nil))
	assertPublicError(t, publishedApproval, http.StatusBadRequest, "APPROVAL_DENIED", "release is not pending approval")

	missingApproval := httptest.NewRecorder()
	router.ServeHTTP(missingApproval, httptest.NewRequest(http.MethodPost, "/v1/workspaces/"+current.ID+"/packages/missing/1.0.0/approve", nil))
	assertPublicError(t, missingApproval, http.StatusNotFound, "APPROVAL_DENIED", "release not found")
}

func assertPublicError(t *testing.T, response *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("expected status %d, got %d: %s", status, response.Code, response.Body.String())
	}
	var envelope struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != code || envelope.Error.Message != message || envelope.Error.RequestID == "" {
		t.Fatalf("unexpected public error: %+v", envelope.Error)
	}
}
