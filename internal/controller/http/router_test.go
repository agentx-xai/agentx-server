package http

import (
	"agentx/server/internal/repo/file"
	"agentx/server/internal/usecase/device"
	"agentx/server/internal/usecase/manifest"
	"agentx/server/internal/usecase/policy"
	"agentx/server/internal/usecase/reconcile"
	"agentx/server/internal/usecase/registry"
	"agentx/server/internal/usecase/workspace"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
)

func TestDeviceRegistration(t *testing.T) {
	dir := t.TempDir()
	s := device.New(file.NewDeviceRepo(dir), file.NewAuditRepo(dir))
	r := NewRouter(s, registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir)))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/devices", bytes.NewBufferString(`{"id":"1","name":"laptop","agent":"codex"}`)))
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/devices", nil))
	if !bytes.Contains(w.Body.Bytes(), []byte("laptop")) {
		t.Fatal("device missing")
	}
}

func TestWorkspaceLifecycle(t *testing.T) {
	dir := t.TempDir()
	d := device.New(file.NewDeviceRepo(dir), file.NewAuditRepo(dir))
	w := workspace.New(file.NewWorkspaceRepo(dir))
	policyService := policy.New(file.NewWorkspaceRepo(dir), w)
	r := NewRouter(d, registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir)), RouterOptions{Workspace: w, Policy: policyService})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/workspaces", bytes.NewBufferString(`{"name":"Platform","slug":"platform"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create workspace: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+created.ID+"/members", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("members: %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("owner")) {
		t.Fatal("owner membership missing")
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+created.ID+"/policies", nil))
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"revision":0`)) {
		t.Fatalf("policy read: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	policyRequest := httptest.NewRequest(http.MethodPut, "/v1/workspaces/"+created.ID+"/policies", bytes.NewBufferString(`{"document":{"require_signature":true}}`))
	policyRequest.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, policyRequest)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"revision":1`)) {
		t.Fatalf("policy write: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/workspaces/"+created.ID+"/members", bytes.NewBufferString(`{"user_id":"issuer|developer","role":"developer"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("add member: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/v1/workspaces/"+created.ID+"/members/issuer%7Cdeveloper", bytes.NewBufferString(`{"role":"viewer"}`)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("update member role: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+created.ID+"/members", nil))
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"role":"viewer"`)) {
		t.Fatalf("updated member role missing: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/workspaces/"+created.ID+"/devices", bytes.NewBufferString(`{"id":"device-1","name":"Build laptop","agent":"Codex","installed_packages":{"skill":"old"}}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register workspace device: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/workspaces/"+created.ID+"/devices/device-1/heartbeat", bytes.NewBufferString(`{"installed_packages":{"skill":"new"}}`)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("workspace heartbeat: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/v1/workspaces/"+created.ID+"/members/issuer%7Cdeveloper", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove member: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/v1/workspaces/"+created.ID, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete workspace: %d %s", rec.Code, rec.Body.String())
	}
}

func TestWorkspaceReleaseIdempotency(t *testing.T) {
	dir := t.TempDir()
	d := device.New(file.NewDeviceRepo(dir), file.NewAuditRepo(dir))
	w := workspace.New(file.NewWorkspaceRepo(dir))
	registryService := registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir))
	registryService.SetWorkspace(w)
	r := NewRouter(d, registryService, RouterOptions{Workspace: w})
	create := httptest.NewRecorder()
	r.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/v1/workspaces", bytes.NewBufferString(`{"name":"Platform","slug":"platform"}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("workspace: %d", create.Code)
	}
	var ws struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &ws); err != nil {
		t.Fatal(err)
	}
	publish := func(data string) *httptest.ResponseRecorder {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		_ = mw.WriteField("version", "1.0.0")
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", `form-data; name="artifact"; filename="skill.tar"`)
		h.Set("Content-Type", "application/octet-stream")
		part, _ := mw.CreatePart(h)
		_, _ = part.Write([]byte(data))
		_ = mw.Close()
		req := httptest.NewRequest(http.MethodPost, "/v1/workspaces/"+ws.ID+"/packages/demo/releases", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Idempotency-Key", "same-request")
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		return out
	}
	first := publish("first")
	if first.Code != http.StatusCreated {
		t.Fatalf("first publish: %d %s", first.Code, first.Body.String())
	}
	approve := httptest.NewRecorder()
	r.ServeHTTP(approve, httptest.NewRequest(http.MethodPost, "/v1/workspaces/"+ws.ID+"/packages/demo/1.0.0/approve", nil))
	if approve.Code != http.StatusOK || !bytes.Contains(approve.Body.Bytes(), []byte(`"status":"approved"`)) {
		t.Fatalf("approve release: %d %s", approve.Code, approve.Body.String())
	}
	second := publish("first")
	if second.Code != http.StatusCreated {
		t.Fatalf("retry publish: %d %s", second.Code, second.Body.String())
	}
	var a, b struct {
		SHA256 string `json:"sha256"`
	}
	_ = json.Unmarshal(first.Body.Bytes(), &a)
	_ = json.Unmarshal(second.Body.Bytes(), &b)
	if a.SHA256 == "" || a.SHA256 != b.SHA256 {
		t.Fatalf("idempotency mismatch: %s %s", a.SHA256, b.SHA256)
	}
	mismatch := publish("second")
	if mismatch.Code != http.StatusBadRequest || !bytes.Contains(mismatch.Body.Bytes(), []byte("different request")) {
		t.Fatalf("expected idempotency mismatch rejection: %d %s", mismatch.Code, mismatch.Body.String())
	}
	artifact := httptest.NewRecorder()
	r.ServeHTTP(artifact, httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+ws.ID+"/artifacts/"+a.SHA256, nil))
	if artifact.Code != http.StatusOK || artifact.Body.String() != "first" {
		t.Fatalf("scoped artifact: %d %q", artifact.Code, artifact.Body.String())
	}
	versionDownload := httptest.NewRecorder()
	r.ServeHTTP(versionDownload, httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+ws.ID+"/packages/demo/1.0.0/download", nil))
	if versionDownload.Code != http.StatusOK || versionDownload.Body.String() != "first" {
		t.Fatalf("version download: %d %q", versionDownload.Code, versionDownload.Body.String())
	}
}

func TestBearerTokenProtectsBusinessRoutes(t *testing.T) {
	t.Setenv("AGENTX_API_TOKEN", "integration-token")
	dir := t.TempDir()
	r := NewRouter(device.New(file.NewDeviceRepo(dir), file.NewAuditRepo(dir)), registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir)))
	unauthorized := httptest.NewRecorder()
	r.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/devices", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", unauthorized.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/devices", nil)
	req.Header.Set("Authorization", "Bearer integration-token")
	authorized := httptest.NewRecorder()
	r.ServeHTTP(authorized, req)
	if authorized.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", authorized.Code)
	}
	me := httptest.NewRecorder()
	meReq := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	meReq.Header.Set("Authorization", "Bearer integration-token")
	r.ServeHTTP(me, meReq)
	if me.Code != http.StatusOK || !bytes.Contains(me.Body.Bytes(), []byte(`"id":"token"`)) {
		t.Fatalf("expected principal, got %d %s", me.Code, me.Body.String())
	}
}

func TestHostedRouterDisablesUnscopedCollections(t *testing.T) {
	dir := t.TempDir()
	workspaces := workspace.New(file.NewWorkspaceRepo(dir))
	router := NewRouter(device.New(file.NewDeviceRepo(dir), file.NewAuditRepo(dir)), registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir)), RouterOptions{Workspace: workspaces, AllowLegacyUnscoped: false})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/packages", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected unscoped package route to be disabled, got %d", response.Code)
	}
}

func TestReconcilePlanAndMetrics(t *testing.T) {
	dir := t.TempDir()
	devices := file.NewDeviceRepo(dir)
	workspaces := workspace.New(file.NewWorkspaceRepo(dir))
	manifests := file.NewWorkspaceRepo(dir)
	r := NewRouter(device.New(devices, file.NewAuditRepo(dir)), registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir)), RouterOptions{Workspace: workspaces, Manifest: manifest.New(manifests, workspaces), Reconcile: reconcile.New(devices, manifests)})
	create := httptest.NewRecorder()
	r.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/v1/workspaces", bytes.NewBufferString(`{"name":"Plan","slug":"plan"}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("workspace: %d %s", create.Code, create.Body.String())
	}
	var ws struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(create.Body.Bytes(), &ws)
	register := httptest.NewRecorder()
	r.ServeHTTP(register, httptest.NewRequest(http.MethodPost, "/v1/workspaces/"+ws.ID+"/devices", bytes.NewBufferString(`{"id":"device-plan","name":"laptop","installed_packages":{"old":"digest"}}`)))
	if register.Code != http.StatusCreated {
		t.Fatalf("device: %d %s", register.Code, register.Body.String())
	}
	manifestReq := httptest.NewRequest(http.MethodPut, "/v1/workspaces/"+ws.ID+"/manifest", bytes.NewBufferString(`{"document":{"packages":[{"name":"new","sha256":"new-digest"}]}}`))
	manifestReq.Header.Set("Content-Type", "application/json")
	manifestResp := httptest.NewRecorder()
	r.ServeHTTP(manifestResp, manifestReq)
	if manifestResp.Code != http.StatusOK {
		t.Fatalf("manifest: %d %s", manifestResp.Code, manifestResp.Body.String())
	}
	plan := httptest.NewRecorder()
	r.ServeHTTP(plan, httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+ws.ID+"/devices/device-plan/plan", nil))
	if plan.Code != http.StatusOK || !bytes.Contains(plan.Body.Bytes(), []byte(`"kind":"install"`)) {
		t.Fatalf("plan: %d %s", plan.Code, plan.Body.String())
	}
	metrics := httptest.NewRecorder()
	r.ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metrics.Code != http.StatusOK || !bytes.Contains(metrics.Body.Bytes(), []byte("agentx_http_requests_total")) {
		t.Fatalf("metrics: %d %s", metrics.Code, metrics.Body.String())
	}
}

func TestPackageRelease(t *testing.T) {
	dir := t.TempDir()
	s := device.New(file.NewDeviceRepo(dir), file.NewAuditRepo(dir))
	r := NewRouter(s, registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir)))
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/packages/demo/releases", bytes.NewBufferString("artifact data"))
	req.Header.Set("Content-Type", "application/octet-stream")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected multipart validation, got %d", w.Code)
	}
}
