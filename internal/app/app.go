package app

import (
	"agentx/server/internal/auth"
	"agentx/server/internal/config"
	httpcontroller "agentx/server/internal/controller/http"
	"agentx/server/internal/job"
	"agentx/server/internal/repo"
	"agentx/server/internal/repo/file"
	"agentx/server/internal/repo/postgres"
	"agentx/server/internal/repo/s3"
	"agentx/server/internal/repo/signature"
	"agentx/server/internal/usecase/device"
	"agentx/server/internal/usecase/drift"
	"agentx/server/internal/usecase/manifest"
	"agentx/server/internal/usecase/policy"
	"agentx/server/internal/usecase/reconcile"
	"agentx/server/internal/usecase/registry"
	"agentx/server/internal/usecase/workspace"
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

type Application struct {
	Address string
	Router  *gin.Engine
	close   func()
}

func (a *Application) Close() {
	if a.close != nil {
		a.close()
	}
}

func New(cfg config.Config) (*Application, error) {
	var devices *device.Service
	var releases *registry.Service
	var workspaces *workspace.Service
	var policies *policy.Service
	var manifests *manifest.Service
	var deviceRepo repo.DeviceRepository
	var packageRepo repo.PackageRepository
	var artifactStore repo.ArtifactStore
	var auditRepo repo.AuditRepository
	var outbox repo.OutboxRepository
	var workspaceRepo repo.WorkspaceRepository
	var policyRepo repo.PolicyRepository
	var manifestRepo repo.ManifestRepository
	var ready func(context.Context) error
	var artifactReady repo.ArtifactReadiness
	closeFn := func() {}
	if cfg.DatabaseURL != "" {
		dbCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		db, err := postgres.Open(dbCtx, cfg.DatabaseURL)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("open PostgreSQL: %w", err)
		}
		closeFn = db.Close
		ready = db.Pool.Ping
		postgresRepo := postgres.WorkspaceRepository{Store: db}
		workspaceRepo = postgresRepo
		policyRepo = postgresRepo
		manifestRepo = postgresRepo
		workspaces = workspace.New(workspaceRepo)
		policies = policy.New(policyRepo, workspaces)
		manifests = manifest.New(manifestRepo, workspaces)
		deviceRepo = postgres.DeviceRepository{Store: db, WorkspaceID: cfg.WorkspaceID}
		packageRepo = postgres.PackageRepository{Store: db, WorkspaceID: cfg.WorkspaceID}
		artifactStore, err = artifactStoreForConfig(cfg)
		if err != nil {
			db.Close()
			return nil, err
		}
		artifactReady, _ = artifactStore.(repo.ArtifactReadiness)
		auditRepo = postgres.AuditRepository{Store: db, WorkspaceID: cfg.WorkspaceID}
		outbox = postgres.OutboxRepo{Store: db}
		devices = device.New(deviceRepo, auditRepo)
		releases = registry.New(artifactStore, packageRepo)
	} else {
		deviceRepo = file.NewDeviceRepo(cfg.DataDir)
		packageRepo = file.NewPackageRepo(cfg.DataDir)
		artifactStore, err := artifactStoreForConfig(cfg)
		if err != nil {
			return nil, err
		}
		artifactReady, _ = artifactStore.(repo.ArtifactReadiness)
		auditRepo = file.NewAuditRepo(cfg.DataDir)
		outbox = file.NewOutboxRepo(cfg.DataDir)
		devices = device.New(deviceRepo, auditRepo)
		releases = registry.New(artifactStore, packageRepo)
		fileRepo := file.NewWorkspaceRepo(cfg.DataDir)
		workspaceRepo = fileRepo
		policyRepo = fileRepo
		manifestRepo = fileRepo
		workspaces = workspace.New(workspaceRepo)
		policies = policy.New(policyRepo, workspaces)
		manifests = manifest.New(manifestRepo, workspaces)
	}
	if cfg.ArtifactPublicKey != "" {
		verifier, err := signature.NewEd25519(cfg.ArtifactPublicKey)
		if err != nil {
			closeFn()
			return nil, fmt.Errorf("parse artifact public key: %w", err)
		}
		releases = registry.New(artifactStore, packageRepo, verifier)
	}
	releases.SetWorkspace(workspaces)
	if policies != nil {
		releases.SetPolicy(policyRepo)
	}
	devices.SetOutbox(outbox)
	workspaces.SetOutbox(outbox)
	policies.SetOutbox(outbox)
	manifests.SetOutbox(outbox)
	releases.SetOutbox(outbox)
	if writer, ok := auditRepo.(repo.WorkspaceAuditWriter); ok {
		workspaces.SetAudit(writer)
		releases.SetAudit(writer)
		policies.SetAudit(writer)
		manifests.SetAudit(writer)
	}
	databaseReady := ready
	ready = func(ctx context.Context) error {
		if databaseReady != nil {
			if err := databaseReady(ctx); err != nil {
				return err
			}
		}
		if artifactReady != nil {
			return artifactReady.Ready(ctx)
		}
		return nil
	}
	workerCtx, workerCancel := context.WithCancel(context.Background())
	worker := job.Worker{Outbox: outbox, Logger: slog.Default()}
	go worker.Run(workerCtx, time.Second)
	previousClose := closeFn
	closeFn = func() {
		workerCancel()
		previousClose()
	}
	var oidcVerifier *auth.OIDCVerifier
	if cfg.OIDCIssuer != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		verifier, err := auth.NewOIDCVerifier(ctx, cfg.OIDCIssuer, cfg.JWTAudience)
		if err != nil {
			closeFn()
			return nil, fmt.Errorf("initialize OIDC verifier: %w", err)
		}
		oidcVerifier = verifier
	}
	return &Application{Address: cfg.Address, Router: httpcontroller.NewRouter(devices, releases, httpcontroller.RouterOptions{Workspace: workspaces, Drift: drift.New(deviceRepo, packageRepo, manifestRepo), Policy: policies, Manifest: manifests, Reconcile: reconcile.New(deviceRepo, manifestRepo), OIDC: oidcVerifier, Ready: ready, AllowLegacyUnscoped: cfg.DatabaseURL == ""}), close: closeFn}, nil
}

func artifactStoreForConfig(cfg config.Config) (repo.ArtifactStore, error) {
	if cfg.ArtifactStore == "s3" {
		store, err := s3.NewArtifactStore(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, cfg.S3Secure)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := store.EnsureBucket(ctx); err != nil {
			return nil, fmt.Errorf("initialize artifact bucket: %w", err)
		}
		return store, nil
	}
	return file.NewArtifactStore(cfg.DataDir), nil
}
