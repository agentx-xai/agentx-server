package http

import (
	"agentx/server/internal/repo/file"
	"agentx/server/internal/usecase/device"
	"agentx/server/internal/usecase/policy"
	"agentx/server/internal/usecase/registry"
	"agentx/server/internal/usecase/workspace"
	"bytes"
	"context"
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
	audit := file.NewAuditRepo(dir)
	outbox := file.NewOutboxRepo(dir)
	registryService.SetPolicy(file.NewWorkspaceRepo(dir))
	registryService.SetWorkspace(workspaces)
	registryService.SetAudit(audit)
	registryService.SetOutbox(outbox)
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
	request.Header.Set("Idempotency-Key", "approval-policy-release")
	router.ServeHTTP(publish, request)
	if publish.Code != http.StatusCreated || !bytes.Contains(publish.Body.Bytes(), []byte(`"pending_approval"`)) {
		t.Fatalf("publish: %d %s", publish.Code, publish.Body.String())
	}
	download := httptest.NewRecorder()
	router.ServeHTTP(download, httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+workspaceID+"/packages/demo/1.0.0/download", nil))
	if download.Code != http.StatusNotFound {
		t.Fatalf("expected gated download, got %d", download.Code)
	}
	approve := httptest.NewRecorder()
	router.ServeHTTP(approve, httptest.NewRequest(http.MethodPost, "/v1/workspaces/"+workspaceID+"/packages/demo/1.0.0/approve", nil))
	if approve.Code != http.StatusOK {
		t.Fatalf("approve pending release: %d %s", approve.Code, approve.Body.String())
	}
	repeated := httptest.NewRecorder()
	router.ServeHTTP(repeated, httptest.NewRequest(http.MethodPost, "/v1/workspaces/"+workspaceID+"/packages/demo/1.0.0/approve", nil))
	if repeated.Code != http.StatusBadRequest || !bytes.Contains(repeated.Body.Bytes(), []byte("release is not pending approval")) {
		t.Fatalf("repeated approval was accepted: %d %s", repeated.Code, repeated.Body.String())
	}
	events, err := audit.ListForWorkspace(context.Background(), workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	approvedEvents := 0
	for _, event := range events {
		if event.Action == "release.approve" {
			approvedEvents++
		}
	}
	if approvedEvents != 1 {
		t.Fatalf("approval audit count = %d, events=%+v", approvedEvents, events)
	}
	queued, err := outbox.Claim(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	approvedMessages := 0
	for _, event := range queued {
		if event.Topic == "release.approved" {
			approvedMessages++
		}
	}
	if approvedMessages != 1 {
		t.Fatalf("approval outbox count = %d, events=%+v", approvedMessages, queued)
	}
}
