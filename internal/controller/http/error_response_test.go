package http

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo/file"
	"agentx/server/internal/usecase/device"
	"agentx/server/internal/usecase/registry"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

const privateBackendError = "postgres password=do-not-disclose"

type failingDeviceRepository struct{}

func (failingDeviceRepository) List(context.Context) ([]entity.Device, error) {
	return nil, errors.New(privateBackendError)
}
func (failingDeviceRepository) Save(context.Context, entity.Device) error { return nil }

func TestInternalErrorsAreLoggedButNotDisclosed(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	dir := t.TempDir()
	router := NewRouter(device.New(failingDeviceRepository{}, file.NewAuditRepo(dir)), registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir)), RouterOptions{
		AllowLegacyUnscoped: true,
		Ready:               func(context.Context) error { return errors.New(privateBackendError) },
	})
	for _, path := range []string{"/v1/devices", "/readyz"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("X-Request-ID", "leak-regression")
		router.ServeHTTP(response, request)
		if response.Code < 500 {
			t.Fatalf("%s returned %d: %s", path, response.Code, response.Body.String())
		}
		if bytes.Contains(response.Body.Bytes(), []byte(privateBackendError)) {
			t.Fatalf("%s disclosed backend error: %s", path, response.Body.String())
		}
		if !bytes.Contains(response.Body.Bytes(), []byte("leak-regression")) {
			t.Fatalf("%s omitted request ID: %s", path, response.Body.String())
		}
	}
	if !bytes.Contains(logs.Bytes(), []byte(privateBackendError)) || !bytes.Contains(logs.Bytes(), []byte("leak-regression")) {
		t.Fatalf("internal error context missing from logs: %s", logs.String())
	}
}

func TestValidationErrorsRemainPublic(t *testing.T) {
	dir := t.TempDir()
	router := NewRouter(device.New(file.NewDeviceRepo(dir), file.NewAuditRepo(dir)), registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir)))
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/devices", bytes.NewBufferString(`{"name":""}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("device name is required")) {
		t.Fatalf("validation response changed: %d %s", response.Code, response.Body.String())
	}
}

func TestRecoveryUsesStableErrorEnvelope(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	router := gin.New()
	router.Use(middleware(nil, logger), recoveryMiddleware(logger))
	router.GET("/panic", func(*gin.Context) { panic(privateBackendError) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if response.Code != http.StatusInternalServerError || bytes.Contains(response.Body.Bytes(), []byte(privateBackendError)) || !bytes.Contains(response.Body.Bytes(), []byte("INTERNAL_ERROR")) {
		t.Fatalf("unsafe recovery response: %d %s", response.Code, response.Body.String())
	}
	if !bytes.Contains(logs.Bytes(), []byte(privateBackendError)) {
		t.Fatalf("panic missing from logs: %s", logs.String())
	}
}
