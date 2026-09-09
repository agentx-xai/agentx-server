package http

import (
	"agentx/server/internal/repo/file"
	"agentx/server/internal/usecase/device"
	"agentx/server/internal/usecase/registry"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type stubRateLimiter struct {
	allowed bool
	err     error
	calls   int
}

func (s *stubRateLimiter) Allow(context.Context, string) (bool, time.Duration, error) {
	s.calls++
	return s.allowed, 17 * time.Second, s.err
}

func TestInjectedRateLimiterRejectsAndFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name       string
		limiter    *stubRateLimiter
		wantStatus int
		wantCode   string
	}{
		{"quota", &stubRateLimiter{}, http.StatusTooManyRequests, "RATE_LIMITED"},
		{"backend", &stubRateLimiter{err: errors.New("database unavailable")}, http.StatusServiceUnavailable, "RATE_LIMIT_UNAVAILABLE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			router := NewRouter(device.New(file.NewDeviceRepo(dir), file.NewAuditRepo(dir)), registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir)), RouterOptions{AllowLegacyUnscoped: true, RateLimiter: test.limiter})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/devices", nil))
			if response.Code != test.wantStatus || !containsJSONCode(response.Body.String(), test.wantCode) {
				t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
			}
			if test.limiter.calls != 1 {
				t.Fatalf("expected limiter call, got %d", test.limiter.calls)
			}
		})
	}
}

func TestRateLimiterBypassesOperationalProbes(t *testing.T) {
	dir := t.TempDir()
	limiter := &stubRateLimiter{err: errors.New("database unavailable")}
	router := NewRouter(device.New(file.NewDeviceRepo(dir), file.NewAuditRepo(dir)), registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir)), RouterOptions{AllowLegacyUnscoped: true, RateLimiter: limiter})
	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("probe %s returned %d", path, response.Code)
		}
	}
	if limiter.calls != 0 {
		t.Fatalf("operational probes consumed rate limit %d times", limiter.calls)
	}
}

func containsJSONCode(body, code string) bool {
	return strings.Contains(body, `"code":"`+code+`"`)
}
