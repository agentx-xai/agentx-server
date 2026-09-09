package http

import (
	"agentx/server/internal/auth"
	"agentx/server/internal/entity"
	"agentx/server/internal/repo/file"
	"agentx/server/internal/usecase/device"
	"agentx/server/internal/usecase/drift"
	"agentx/server/internal/usecase/manifest"
	"agentx/server/internal/usecase/policy"
	"agentx/server/internal/usecase/reconcile"
	"agentx/server/internal/usecase/registry"
	"agentx/server/internal/usecase/workspace"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

type rbacCase struct {
	name     string
	method   string
	path     string
	body     string
	required entity.Role
	headers  map[string]string
}

func TestWorkspaceRBACMatrix(t *testing.T) {
	roles := []entity.Role{entity.RoleViewer, entity.RoleDeveloper, entity.RoleAdmin, entity.RoleOwner}
	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			dir := t.TempDir()
			workspaceRepo := file.NewWorkspaceRepo(dir)
			workspaceService := workspace.New(workspaceRepo)
			deviceRepo := file.NewDeviceRepo(dir)
			packageRepo := file.NewPackageRepo(dir)
			auditRepo := file.NewAuditRepo(dir)
			deviceService := device.New(deviceRepo, auditRepo)
			policyService := policy.New(workspaceRepo, workspaceService)
			manifestService := manifest.New(workspaceRepo, workspaceService)
			registryService := registry.New(file.NewArtifactStore(dir), packageRepo)
			registryService.SetWorkspace(workspaceService)
			registryService.SetPolicy(workspaceRepo)
			router := NewRouter(deviceService, registryService, RouterOptions{
				Workspace: workspaceService,
				Policy:    policyService,
				Manifest:  manifestService,
				Drift:     drift.New(deviceRepo, packageRepo, workspaceRepo),
				Reconcile: reconcile.New(deviceRepo, workspaceRepo),
			})

			workspaceID := uuid.NewString()
			principalID := "rbac|" + string(role)
			if err := workspaceRepo.Create(context.Background(), entity.Workspace{ID: workspaceID, Name: "RBAC", Slug: "rbac-" + string(role), CreatedAt: time.Now().UTC()}, entity.Membership{WorkspaceID: workspaceID, UserID: "rbac|owner", Role: entity.RoleOwner, CreatedAt: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			if role != entity.RoleOwner {
				if err := workspaceRepo.AddMember(context.Background(), entity.Membership{WorkspaceID: workspaceID, UserID: principalID, Role: role, CreatedAt: time.Now().UTC()}); err != nil {
					t.Fatal(err)
				}
			} else {
				principalID = "rbac|owner"
			}
			for _, userID := range []string{"rbac|patch-target", "rbac|delete-target"} {
				if err := workspaceRepo.AddMember(context.Background(), entity.Membership{WorkspaceID: workspaceID, UserID: userID, Role: entity.RoleViewer, CreatedAt: time.Now().UTC()}); err != nil {
					t.Fatal(err)
				}
			}
			invitationID := uuid.NewString()
			if err := workspaceRepo.CreateInvitation(context.Background(), entity.WorkspaceInvitation{ID: invitationID, WorkspaceID: workspaceID, Email: "pending-" + string(role) + "@example.com", Role: entity.RoleViewer, CreatedBy: "rbac|owner", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			if err := deviceRepo.SaveForWorkspace(context.Background(), workspaceID, entity.Device{ID: "existing-device", Name: "Existing", InstalledPackages: map[string]string{}}); err != nil {
				t.Fatal(err)
			}
			if err := packageRepo.SaveForWorkspace(context.Background(), workspaceID, entity.Release{Name: "pending", Version: "1.0.0", SHA256: "digest", Status: "pending_approval", CreatedAt: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}

			prefix := "/v1/workspaces/" + workspaceID
			cases := []rbacCase{
				{name: "members read", method: http.MethodGet, path: prefix + "/members", required: entity.RoleViewer},
				{name: "invitations read", method: http.MethodGet, path: prefix + "/invitations", required: entity.RoleAdmin},
				{name: "invitation create", method: http.MethodPost, path: prefix + "/invitations", body: `{"email":"new-member@example.com","role":"viewer"}`, required: entity.RoleAdmin},
				{name: "invitation revoke", method: http.MethodDelete, path: prefix + "/invitations/" + invitationID, required: entity.RoleAdmin},
				{name: "member role", method: http.MethodPatch, path: prefix + "/members/rbac%7Cpatch-target", body: `{"role":"developer"}`, required: entity.RoleAdmin},
				{name: "member remove", method: http.MethodDelete, path: prefix + "/members/rbac%7Cdelete-target", required: entity.RoleAdmin},
				{name: "packages read", method: http.MethodGet, path: prefix + "/packages", required: entity.RoleViewer},
				{name: "package publish", method: http.MethodPost, path: prefix + "/packages/new/releases", required: entity.RoleDeveloper, headers: map[string]string{"Idempotency-Key": "rbac-test"}},
				{name: "artifact read", method: http.MethodGet, path: prefix + "/artifacts/missing", required: entity.RoleViewer},
				{name: "version download", method: http.MethodGet, path: prefix + "/packages/missing/1.0.0/download", required: entity.RoleViewer},
				{name: "release approve", method: http.MethodPost, path: prefix + "/packages/pending/1.0.0/approve", required: entity.RoleAdmin},
				{name: "devices read", method: http.MethodGet, path: prefix + "/devices", required: entity.RoleViewer},
				{name: "device register", method: http.MethodPost, path: prefix + "/devices", body: `{"id":"new-device","name":"New"}`, required: entity.RoleDeveloper, headers: map[string]string{"Idempotency-Key": "rbac-device"}},
				{name: "device heartbeat", method: http.MethodPost, path: prefix + "/devices/existing-device/heartbeat", body: `{}`, required: entity.RoleDeveloper},
				{name: "drift read", method: http.MethodGet, path: prefix + "/drift", required: entity.RoleViewer},
				{name: "audit read", method: http.MethodGet, path: prefix + "/audit-events", required: entity.RoleViewer},
				{name: "policy read", method: http.MethodGet, path: prefix + "/policies", required: entity.RoleViewer},
				{name: "policy update", method: http.MethodPut, path: prefix + "/policies", body: `{"document":{}}`, required: entity.RoleAdmin},
				{name: "manifest read", method: http.MethodGet, path: prefix + "/manifest", required: entity.RoleViewer},
				{name: "manifest update", method: http.MethodPut, path: prefix + "/manifest", body: `{"document":{"version":1,"packages":[]}}`, required: entity.RoleAdmin},
				{name: "reconcile plan", method: http.MethodGet, path: prefix + "/devices/existing-device/plan", required: entity.RoleViewer},
				{name: "workspace delete", method: http.MethodDelete, path: prefix, required: entity.RoleOwner},
			}
			for _, testCase := range cases {
				t.Run(testCase.name, func(t *testing.T) {
					request := httptest.NewRequest(testCase.method, testCase.path, bytes.NewBufferString(testCase.body))
					request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{UserID: principalID}))
					if testCase.body != "" {
						request.Header.Set("Content-Type", "application/json")
					}
					for name, value := range testCase.headers {
						request.Header.Set(name, value)
					}
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					allowed := role.Allows(testCase.required)
					if allowed && response.Code == http.StatusForbidden {
						t.Fatalf("%s should allow %s but returned 403: %s", role, testCase.required, response.Body.String())
					}
					if !allowed && response.Code != http.StatusForbidden {
						t.Fatalf("%s should deny %s but returned %d: %s", role, testCase.required, response.Code, response.Body.String())
					}
				})
			}
		})
	}
}
