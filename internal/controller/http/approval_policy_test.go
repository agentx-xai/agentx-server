package http

import (
	"agentx/server/internal/repo/file"
	"agentx/server/internal/usecase/device"
	"agentx/server/internal/usecase/policy"
	"agentx/server/internal/usecase/registry"
	"agentx/server/internal/usecase/workspace"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApprovalPolicyGatesDownload(t *testing.T) {
	dir := t.TempDir()
	workspaces := workspace.New(file.NewWorkspaceRepo(dir))
	policies := policy.New(file.NewWorkspaceRepo(dir), workspaces)
	registryService := registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir))
	registryService.SetPolicy(file.NewWorkspaceRepo(dir))
	router := NewRouter(device.New(file.NewDeviceRepo(dir), file.NewAuditRepo(dir)), registryService, RouterOptions{Workspace: workspaces, Policy: policies})
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
	policyUpdate := httptest.NewRecorder()
	policyRequest := httptest.NewRequest(http.MethodPut, "/v1/workspaces/"+workspaceID+"/policies", bytes.NewBufferString(`{"document":{"require_approval":true}}`))
	policyRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(policyUpdate, policyRequest)
	if policyUpdate.Code != http.StatusOK {
		t.Fatalf("policy update: %d %s", policyUpdate.Code, policyUpdate.Body.String())
	}
	var body bytes.Buffer
	body.WriteString("--boundary\r\nContent-Disposition: form-data; name=\"version\"\r\n\r\n1.0.0\r\n")
	body.WriteString("--boundary\r\nContent-Disposition: form-data; name=\"artifact\"; filename=\"demo\"\r\nContent-Type: application/octet-stream\r\n\r\nhello\r\n--boundary--\r\n")
	publish := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/workspaces/"+workspaceID+"/packages/demo/releases", &body)
	request.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")
	router.ServeHTTP(publish, request)
	if publish.Code != http.StatusCreated || !bytes.Contains(publish.Body.Bytes(), []byte(`"pending_approval"`)) {
		t.Fatalf("publish: %d %s", publish.Code, publish.Body.String())
	}
	download := httptest.NewRecorder()
	router.ServeHTTP(download, httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+workspaceID+"/packages/demo/1.0.0/download", nil))
	if download.Code != http.StatusNotFound {
		t.Fatalf("expected gated download, got %d", download.Code)
	}
}
