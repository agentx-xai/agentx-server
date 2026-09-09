package http

import (
	"agentx/server/internal/auth"
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
	"time"

	"github.com/google/uuid"
)

func invitationRequest(method, path, body string, principal auth.Principal) *http.Request {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request.WithContext(auth.WithPrincipal(request.Context(), principal))
}

func TestVerifiedEmailInvitationLifecycle(t *testing.T) {
	dir := t.TempDir()
	workspaceRepo := file.NewWorkspaceRepo(dir)
	auditRepo := file.NewAuditRepo(dir)
	workspaceService := workspace.New(workspaceRepo)
	workspaceService.SetAudit(auditRepo)
	workspaceService.SetOutbox(file.NewOutboxRepo(dir))
	router := NewRouter(
		device.New(file.NewDeviceRepo(dir), auditRepo),
		registry.New(file.NewArtifactStore(dir), file.NewPackageRepo(dir)),
		RouterOptions{Workspace: workspaceService},
	)
	owner := auth.Principal{UserID: "issuer|owner", Issuer: "issuer", Subject: "owner", Email: "owner@example.com", EmailVerified: true}
	invitee := auth.Principal{UserID: "issuer|invitee", Issuer: "issuer", Subject: "invitee", Email: "Invitee@Example.com", EmailVerified: true}

	response := httptest.NewRecorder()
	router.ServeHTTP(response, invitationRequest(http.MethodPost, "/v1/workspaces", `{"name":"Invitations","slug":"invitations"}`, owner))
	if response.Code != http.StatusCreated {
		t.Fatalf("create workspace: %d %s", response.Code, response.Body.String())
	}
	var created entity.Workspace
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, invitationRequest(http.MethodPost, "/v1/workspaces/"+created.ID+"/members", `{"user_id":"issuer|bypass","role":"viewer"}`, owner))
	if response.Code != http.StatusNotFound {
		t.Fatalf("principal-ID membership bypass remains available: %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, invitationRequest(http.MethodPost, "/v1/workspaces/"+created.ID+"/invitations", `{"email":"Invitee@Example.com","role":"developer","expires_in_seconds":3600}`, owner))
	if response.Code != http.StatusCreated {
		t.Fatalf("create invitation: %d %s", response.Code, response.Body.String())
	}
	var invitation entity.WorkspaceInvitation
	if err := json.Unmarshal(response.Body.Bytes(), &invitation); err != nil {
		t.Fatal(err)
	}
	if invitation.Email != "invitee@example.com" || invitation.Status != "pending" || invitation.Role != entity.RoleDeveloper {
		t.Fatalf("unexpected invitation: %+v", invitation)
	}

	unverified := invitee
	unverified.EmailVerified = false
	response = httptest.NewRecorder()
	router.ServeHTTP(response, invitationRequest(http.MethodGet, "/v1/invitations", "", unverified))
	if response.Code != http.StatusForbidden {
		t.Fatalf("unverified email listed invitations: %d %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, invitationRequest(http.MethodPost, "/v1/invitations/"+invitation.ID+"/claim", "", unverified))
	if response.Code != http.StatusForbidden {
		t.Fatalf("unverified email claimed invitation: %d %s", response.Code, response.Body.String())
	}

	wrongEmail := invitee
	wrongEmail.Email = "other@example.com"
	response = httptest.NewRecorder()
	router.ServeHTTP(response, invitationRequest(http.MethodPost, "/v1/invitations/"+invitation.ID+"/claim", "", wrongEmail))
	if response.Code != http.StatusNotFound {
		t.Fatalf("wrong email learned or claimed invitation: %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, invitationRequest(http.MethodGet, "/v1/invitations?limit=1", "", invitee))
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(invitation.ID)) || !bytes.Contains(response.Body.Bytes(), []byte(`"workspace_name":"Invitations"`)) {
		t.Fatalf("matching invitation not listed: %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, invitationRequest(http.MethodPost, "/v1/invitations/"+invitation.ID+"/claim", "", invitee))
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"status":"accepted"`)) {
		t.Fatalf("claim invitation: %d %s", response.Code, response.Body.String())
	}
	membership, err := workspaceRepo.Membership(context.Background(), created.ID, invitee.UserID)
	if err != nil || membership.Role != entity.RoleDeveloper {
		t.Fatalf("claimed membership missing: %+v %v", membership, err)
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, invitationRequest(http.MethodPost, "/v1/invitations/"+invitation.ID+"/claim", "", invitee))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("accepted invitation claimed twice: %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, invitationRequest(http.MethodPost, "/v1/workspaces/"+created.ID+"/invitations", `{"email":"revoked@example.com","role":"viewer"}`, owner))
	if response.Code != http.StatusCreated {
		t.Fatalf("create revocable invitation: %d %s", response.Code, response.Body.String())
	}
	var revocable entity.WorkspaceInvitation
	if err := json.Unmarshal(response.Body.Bytes(), &revocable); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, invitationRequest(http.MethodDelete, "/v1/workspaces/"+created.ID+"/invitations/"+revocable.ID, "", owner))
	if response.Code != http.StatusNoContent {
		t.Fatalf("revoke invitation: %d %s", response.Code, response.Body.String())
	}

	expired := entity.WorkspaceInvitation{ID: uuid.NewString(), WorkspaceID: created.ID, Email: "expired@example.com", Role: entity.RoleViewer, CreatedBy: owner.UserID, CreatedAt: time.Now().UTC().Add(-2 * time.Hour), ExpiresAt: time.Now().UTC().Add(-time.Hour)}
	if err := workspaceRepo.CreateInvitation(context.Background(), expired); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, invitationRequest(http.MethodPost, "/v1/invitations/"+expired.ID+"/claim", "", auth.Principal{UserID: "issuer|expired", Issuer: "issuer", Subject: "expired", Email: expired.Email, EmailVerified: true}))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expired invitation claimed: %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, invitationRequest(http.MethodGet, "/v1/workspaces/"+created.ID+"/invitations?limit=10", "", owner))
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"status":"accepted"`)) || !bytes.Contains(response.Body.Bytes(), []byte(`"status":"revoked"`)) || !bytes.Contains(response.Body.Bytes(), []byte(`"status":"expired"`)) {
		t.Fatalf("admin invitation history incomplete: %d %s", response.Code, response.Body.String())
	}

	events, err := auditRepo.ListForWorkspace(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]bool{}
	for _, event := range events {
		actions[event.Action] = true
	}
	for _, action := range []string{"invitation.create", "invitation.claim", "invitation.revoke"} {
		if !actions[action] {
			t.Fatalf("missing invitation audit action %s: %+v", action, events)
		}
	}
}
