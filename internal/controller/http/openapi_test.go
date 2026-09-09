package http

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo"
	"agentx/server/internal/repo/file"
	accountusecase "agentx/server/internal/usecase/account"
	"agentx/server/internal/usecase/device"
	"agentx/server/internal/usecase/drift"
	legalholdusecase "agentx/server/internal/usecase/legalhold"
	"agentx/server/internal/usecase/manifest"
	"agentx/server/internal/usecase/policy"
	"agentx/server/internal/usecase/reconcile"
	"agentx/server/internal/usecase/registry"
	"agentx/server/internal/usecase/workspace"
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type openAPIAccountRepository struct{}

func (openAPIAccountRepository) ExportAccount(context.Context, string, string) (entity.AccountExport, error) {
	return entity.AccountExport{}, nil
}
func (openAPIAccountRepository) DeleteAccount(context.Context, entity.AccountDeletion) error {
	return nil
}

var _ repo.AccountRepository = openAPIAccountRepository{}

func TestEveryHTTPRouteIsDocumentedInOpenAPI(t *testing.T) {
	dir := t.TempDir()
	workspaceRepo := file.NewWorkspaceRepo(dir)
	workspaceService := workspace.New(workspaceRepo)
	deviceRepo := file.NewDeviceRepo(dir)
	packageRepo := file.NewPackageRepo(dir)
	router := NewRouter(
		device.New(deviceRepo, file.NewAuditRepo(dir)),
		registry.New(file.NewArtifactStore(dir), packageRepo),
		RouterOptions{
			Account: accountusecase.New(openAPIAccountRepository{}), LegalHold: legalholdusecase.New(&legalHoldHTTPRepository{}, []string{"compliance"}), Workspace: workspaceService,
			Policy: policy.New(workspaceRepo, workspaceService), Manifest: manifest.New(workspaceRepo, workspaceService),
			Drift: drift.New(deviceRepo, packageRepo, workspaceRepo), Reconcile: reconcile.New(deviceRepo, workspaceRepo),
			AllowLegacyUnscoped: true,
		},
	)
	raw, err := os.ReadFile("../../../openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	parameter := regexp.MustCompile(`:([A-Za-z0-9_]+)`)
	for _, route := range router.Routes() {
		path := parameter.ReplaceAllString(route.Path, `{$1}`)
		operations, found := spec.Paths[path]
		if !found {
			t.Errorf("route %s %s is missing from OpenAPI", route.Method, path)
			continue
		}
		if _, found := operations[strings.ToLower(route.Method)]; !found {
			t.Errorf("operation %s %s is missing from OpenAPI", route.Method, path)
		}
	}
}
