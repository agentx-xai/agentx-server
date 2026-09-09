package http

import (
	"agentx/server/internal/auth"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo/file"
	"agentx/server/internal/usecase/device"
	"agentx/server/internal/usecase/drift"
	"agentx/server/internal/usecase/registry"
	"agentx/server/internal/usecase/workspace"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRequestLogIncludesTenantContext(t *testing.T) {
	dir := t.TempDir()
	workspaceID := uuid.NewString()
	actorID := "issuer|subject"
	workspaceRepo := file.NewWorkspaceRepo(dir)
	if err := workspaceRepo.Create(context.Background(), entity.Workspace{ID: workspaceID, Name: "Logs", Slug: "logs", CreatedAt: time.Now().UTC()}, entity.Membership{WorkspaceID: workspaceID, UserID: actorID, Role: entity.RoleOwner, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	router := NewRouter(device.New(file.NewDeviceRepo(dir), file.NewAuditRepo(dir)), registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir)), RouterOptions{Workspace: workspace.New(workspaceRepo), Logger: logger})
	request := httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+workspaceID+"/devices", nil)
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{UserID: actorID}))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("request failed: %d %s", response.Code, response.Body.String())
	}
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatalf("request log is not JSON: %v: %s", err, output.String())
	}
	for key, expected := range map[string]any{"msg": "http_request", "route": "/v1/workspaces/:id/devices", "actor_id": actorID, "workspace_id": workspaceID, "method": http.MethodGet, "status": float64(http.StatusOK)} {
		if record[key] != expected {
			t.Fatalf("log field %s=%v, want %v: %s", key, record[key], expected, output.String())
		}
	}
	if record["request_id"] == "" {
		t.Fatalf("request ID missing from log: %s", output.String())
	}
}

func TestDomainMetricsCountUploadFailuresAndDriftReports(t *testing.T) {
	dir := t.TempDir()
	deviceRepo := file.NewDeviceRepo(dir)
	router := NewRouter(device.New(deviceRepo, file.NewAuditRepo(dir)), registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir)), RouterOptions{AllowLegacyUnscoped: true, Drift: drift.New(deviceRepo, file.NewPackageRepo(dir), nil)})
	uploadBefore := metrics.uploadFailures.Load()
	driftBefore := metrics.driftReports.Load()
	upload := httptest.NewRecorder()
	router.ServeHTTP(upload, httptest.NewRequest(http.MethodPost, "/v1/packages/demo/releases", bytes.NewBufferString("invalid")))
	driftResponse := httptest.NewRecorder()
	router.ServeHTTP(driftResponse, httptest.NewRequest(http.MethodGet, "/v1/drift", nil))
	if upload.Code < 400 || driftResponse.Code != http.StatusOK {
		t.Fatalf("unexpected responses: upload=%d drift=%d", upload.Code, driftResponse.Code)
	}
	if metrics.uploadFailures.Load() != uploadBefore+1 || metrics.driftReports.Load() != driftBefore+1 {
		t.Fatalf("domain metrics not incremented: upload=%d drift=%d", metrics.uploadFailures.Load()-uploadBefore, metrics.driftReports.Load()-driftBefore)
	}
	metricsResponse := httptest.NewRecorder()
	router.ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, name := range []string{"agentx_artifact_upload_failures_total", "agentx_drift_reports_total"} {
		if !bytes.Contains(metricsResponse.Body.Bytes(), []byte(name)) {
			t.Fatalf("metric %s missing: %s", name, metricsResponse.Body.String())
		}
	}
}

func TestRequestEmitsRouteSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	}()

	router := NewRouter(device.New(file.NewDeviceRepo(t.TempDir()), file.NewAuditRepo(t.TempDir())), registry.New(file.NewArtifactStore(t.TempDir()), file.NewPackageRepo(t.TempDir())))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("request failed: %d", response.Code)
	}
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "GET /healthz" {
		t.Fatalf("unexpected HTTP spans: %#v", spans)
	}
}
