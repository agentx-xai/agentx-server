package http

import (
	"agentx/server/internal/auth"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	accountusecase "agentx/server/internal/usecase/account"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type accountRepositoryStub struct {
	export    entity.AccountExport
	deletion  entity.AccountDeletion
	deleteErr error
}

func (r *accountRepositoryStub) ExportAccount(context.Context, string, string) (entity.AccountExport, error) {
	return r.export, nil
}

func (r *accountRepositoryStub) DeleteAccount(_ context.Context, deletion entity.AccountDeletion) error {
	r.deletion = deletion
	return r.deleteErr
}

func accountRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	return request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{UserID: "issuer|subject", Issuer: "issuer", Subject: "subject", Email: "person@example.com", EmailVerified: true}))
}

func TestAccountExportAndDeletionRoutes(t *testing.T) {
	repository := &accountRepositoryStub{}
	router := NewRouter(nil, nil, RouterOptions{Account: accountusecase.New(repository)})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, accountRequest(http.MethodGet, "/v1/me/export", ""))
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Disposition") != `attachment; filename="agentx-account-export.json"` || !bytes.Contains(response.Body.Bytes(), []byte(`"schema_version":1`)) {
		t.Fatalf("unexpected account export: %d %#v %s", response.Code, response.Header(), response.Body.String())
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, accountRequest(http.MethodDelete, "/v1/me", `{"confirmation":"delete"}`))
	if response.Code != http.StatusBadRequest || repository.deletion.UserID != "" {
		t.Fatalf("invalid confirmation reached repository: %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, accountRequest(http.MethodDelete, "/v1/me", `{"confirmation":"DELETE"}`))
	if response.Code != http.StatusNoContent || repository.deletion.UserID != "issuer|subject" {
		t.Fatalf("account deletion failed: %d %s %+v", response.Code, response.Body.String(), repository.deletion)
	}

	repository.deleteErr = repo.Conflict("account owns a workspace")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, accountRequest(http.MethodDelete, "/v1/me", `{"confirmation":"DELETE"}`))
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("delete or transfer owned workspaces")) {
		t.Fatalf("owner conflict was not public and stable: %d %s", response.Code, response.Body.String())
	}
}
