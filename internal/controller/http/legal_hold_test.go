package http

import (
	"agentx/server/internal/auth"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	legalholdusecase "agentx/server/internal/usecase/legalhold"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type legalHoldHTTPRepository struct {
	hold entity.LegalHold
}

func (r *legalHoldHTTPRepository) CreateLegalHold(_ context.Context, hold entity.LegalHold, _ entity.AuditEvent, _ entity.OutboxEvent) error {
	r.hold = hold
	return nil
}
func (r *legalHoldHTTPRepository) LegalHold(context.Context, string) (entity.LegalHold, error) {
	return r.hold, nil
}
func (r *legalHoldHTTPRepository) ListLegalHoldsPage(context.Context, repo.LegalHoldFilter, repo.PageRequest) (repo.Page[entity.LegalHold], error) {
	return repo.Page[entity.LegalHold]{Items: []entity.LegalHold{r.hold}, Total: 1}, nil
}
func (r *legalHoldHTTPRepository) ReleaseLegalHold(_ context.Context, id, releasedBy, reason string, at time.Time, _ entity.AuditEvent, _ entity.OutboxEvent) (entity.LegalHold, error) {
	r.hold.ID, r.hold.ReleasedBy, r.hold.ReleasedAt, r.hold.ReleaseReason = id, releasedBy, &at, reason
	return r.hold, nil
}

func legalHoldRouter(repository *legalHoldHTTPRepository) http.Handler {
	service := legalholdusecase.New(repository, []string{"issuer|compliance"})
	router := NewRouter(nil, nil, RouterOptions{LegalHold: service})
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{UserID: request.Header.Get("X-Test-Principal")}))
		router.ServeHTTP(response, request)
	})
}

func TestLegalHoldHTTPRequiresComplianceAdministrator(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/admin/legal-holds", strings.NewReader(`{"target_type":"account","target_id":"issuer|user","reason":"investigation"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Test-Principal", "issuer|tenant-owner")
	legalHoldRouter(&legalHoldHTTPRepository{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("tenant owner should be forbidden, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestLegalHoldHTTPCreateListAndRelease(t *testing.T) {
	repository := &legalHoldHTTPRepository{}
	router := legalHoldRouter(repository)
	create := httptest.NewRequest(http.MethodPost, "/v1/admin/legal-holds", strings.NewReader(`{"target_type":"account","target_id":"issuer|user","reason":"investigation"}`))
	create.Header.Set("Content-Type", "application/json")
	create.Header.Set("X-Test-Principal", "issuer|compliance")
	created := httptest.NewRecorder()
	router.ServeHTTP(created, create)
	if created.Code != http.StatusCreated || repository.hold.ID == "" {
		t.Fatalf("create failed: %d %s", created.Code, created.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "/v1/admin/legal-holds?status=active", nil)
	list.Header.Set("X-Test-Principal", "issuer|compliance")
	listed := httptest.NewRecorder()
	router.ServeHTTP(listed, list)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), repository.hold.ID) {
		t.Fatalf("list failed: %d %s", listed.Code, listed.Body.String())
	}

	release := httptest.NewRequest(http.MethodPost, "/v1/admin/legal-holds/"+repository.hold.ID+"/release", strings.NewReader(`{"confirmation":"RELEASE","reason":"matter closed"}`))
	release.Header.Set("Content-Type", "application/json")
	release.Header.Set("X-Test-Principal", "issuer|compliance")
	released := httptest.NewRecorder()
	router.ServeHTTP(released, release)
	if released.Code != http.StatusOK || repository.hold.ReleasedAt == nil {
		t.Fatalf("release failed: %d %s", released.Code, released.Body.String())
	}
}
