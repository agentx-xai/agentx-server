package app

import (
	"agentx/server/internal/auth"
	"agentx/server/internal/config"
	httpcontroller "agentx/server/internal/controller/http"
	"agentx/server/internal/job"
	"agentx/server/internal/notification"
	"agentx/server/internal/repo"
	"agentx/server/internal/repo/file"
	"agentx/server/internal/repo/postgres"
	"agentx/server/internal/repo/s3"
	"agentx/server/internal/repo/signature"
	"agentx/server/internal/telemetry"
	"agentx/server/internal/usecase/account"
	"agentx/server/internal/usecase/device"
	"agentx/server/internal/usecase/drift"
	"agentx/server/internal/usecase/legalhold"
	"agentx/server/internal/usecase/manifest"
	"agentx/server/internal/usecase/policy"
	"agentx/server/internal/usecase/reconcile"
	"agentx/server/internal/usecase/registry"
	"agentx/server/internal/usecase/workspace"
	"context"
	"fmt"
	"log/slog"
	"sync"
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
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	var devices *device.Service
	var accounts *account.Service
	var releases *registry.Service
	var workspaces *workspace.Service
	var policies *policy.Service
	var manifests *manifest.Service
	var legalHolds *legalhold.Service
	var auditRetention repo.AuditRetentionRepository
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
	var unitOfWork repo.UnitOfWork
	var requestLimiter httpcontroller.RequestRateLimiter
	closeFn := func() {}
	if cfg.DatabaseURL != "" {
		dbCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		db, err := postgres.Open(dbCtx, cfg.DatabaseURL)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("open PostgreSQL: %w", err)
		}
		closeFn = db.Close
		unitOfWork = db
		if cfg.RateLimitPerMinute > 0 {
			requestLimiter = postgres.NewRateLimiter(db, cfg.RateLimitPerMinute)
		}
		ready = db.Pool.Ping
		postgresRepo := postgres.WorkspaceRepository{Store: db}
		accounts = account.New(postgres.AccountRepository{Store: db})
		legalHolds = legalhold.New(postgres.LegalHoldRepository{Store: db}, cfg.ComplianceAdminIDs)
		auditRetention = postgres.AuditRetentionRepository{Store: db}
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
	artifactPublicKeys := append([]string(nil), cfg.ArtifactPublicKeys...)
	artifactStore = telemetry.TraceArtifactStore(artifactStore)
	artifactReady = artifactStore.(repo.ArtifactReadiness)
	releases = registry.New(artifactStore, packageRepo)
	if cfg.ArtifactPublicKey != "" {
		artifactPublicKeys = append(artifactPublicKeys, cfg.ArtifactPublicKey)
	}
	if len(artifactPublicKeys) > 0 {
		verifier, err := signature.NewEd25519Keyring(artifactPublicKeys)
		if err != nil {
			closeFn()
			return nil, fmt.Errorf("parse artifact public keys: %w", err)
		}
		releases = registry.New(artifactStore, packageRepo, verifier)
	}
	policies.SetSignatureVerificationAvailable(len(artifactPublicKeys) > 0)
	releases.SetWorkspace(workspaces)
	releases.SetPackageArchiveRequired(cfg.DeploymentMode == config.ModeHosted)
	manifests.SetPackages(packageRepo)
	artifactLifecycle, ok := packageRepo.(repo.ArtifactLifecycleRepository)
	if !ok {
		closeFn()
		return nil, fmt.Errorf("artifact lifecycle repository is unavailable")
	}
	workspaces.SetArtifactLifecycle(artifactLifecycle)
	if policies != nil {
		releases.SetPolicy(policyRepo)
	}
	devices.SetOutbox(outbox)
	workspaces.SetOutbox(outbox)
	policies.SetOutbox(outbox)
	manifests.SetOutbox(outbox)
	releases.SetOutbox(outbox)
	if unitOfWork != nil {
		devices.SetUnitOfWork(unitOfWork)
		workspaces.SetUnitOfWork(unitOfWork)
		policies.SetUnitOfWork(unitOfWork)
		manifests.SetUnitOfWork(unitOfWork)
		releases.SetUnitOfWork(unitOfWork)
	}
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
	workerLogger := slog.Default()
	workerMetrics := &job.Metrics{}
	var workers sync.WaitGroup
	artifactCleanup := job.ArtifactCleanupHandler{References: artifactLifecycle, Store: artifactStore, Logger: workerLogger}
	var invitationDelivery *job.InvitationDeliveryHandler
	if cfg.SMTPAddress != "" {
		sender, senderErr := notification.NewSMTPInvitationSender(notification.SMTPConfig{
			Address: cfg.SMTPAddress, Username: cfg.SMTPUsername, Password: cfg.SMTPPassword,
			From: cfg.SMTPFrom, TLSMode: cfg.SMTPTLSMode, ConsoleURL: cfg.ConsoleURL,
		})
		if senderErr != nil {
			workerCancel()
			closeFn()
			return nil, fmt.Errorf("initialize invitation delivery: %w", senderErr)
		}
		invitationRepo, invitationOK := workspaceRepo.(repo.WorkspaceInvitationRepository)
		if !invitationOK {
			workerCancel()
			closeFn()
			return nil, fmt.Errorf("invitation delivery repository is unavailable")
		}
		invitationDelivery = &job.InvitationDeliveryHandler{Invitations: invitationRepo, Sender: sender}
	}
	worker := job.Worker{Outbox: outbox, Logger: workerLogger, Metrics: workerMetrics, Handler: func(ctx context.Context, topic string, payload map[string]any) error {
		switch topic {
		case "artifact.cleanup":
			return artifactCleanup.Handle(ctx, payload)
		case "invitation.created":
			if invitationDelivery != nil {
				return invitationDelivery.Handle(ctx, payload)
			}
			workerLogger.WarnContext(ctx, "invitation delivery disabled", "invitation_id", payload["invitation_id"])
			return nil
		}
		workerLogger.InfoContext(ctx, "outbox event delivered", "topic", topic, "payload", payload)
		return nil
	}}
	workers.Add(1)
	go func() {
		defer workers.Done()
		worker.Run(workerCtx, time.Second)
	}()
	if auditRetention != nil && cfg.AuditRetentionDays > 0 {
		retentionWorker := job.AuditRetentionWorker{Repository: auditRetention, RetentionDays: cfg.AuditRetentionDays, Logger: workerLogger}
		workers.Add(1)
		go func() {
			defer workers.Done()
			retentionWorker.Run(workerCtx, cfg.AuditRetentionInterval)
		}()
	}
	workersDone := make(chan struct{})
	go func() {
		workers.Wait()
		close(workersDone)
	}()
	previousClose := closeFn
	closeFn = func() {
		workerCancel()
		select {
		case <-workersDone:
		case <-time.After(5 * time.Second):
			workerLogger.Warn("worker shutdown exceeded drain timeout")
		}
		previousClose()
	}
	var oidcVerifier *auth.OIDCVerifier
	if cfg.OIDCIssuer != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		verifier, err := auth.NewOIDCVerifier(ctx, cfg.OIDCIssuer, cfg.JWTAudience, cfg.OIDCBackchannelURL)
		if err != nil {
			closeFn()
			return nil, fmt.Errorf("initialize OIDC verifier: %w", err)
		}
		oidcVerifier = verifier
	}
	return &Application{Address: cfg.Address, Router: httpcontroller.NewRouter(devices, releases, httpcontroller.RouterOptions{
		Account: accounts, LegalHold: legalHolds, Workspace: workspaces, Drift: drift.New(deviceRepo, packageRepo, manifestRepo), Policy: policies,
		Manifest: manifests, Reconcile: reconcile.New(deviceRepo, manifestRepo), OIDC: oidcVerifier, Ready: ready,
		AllowLegacyUnscoped: cfg.AllowLegacyUnscoped, APIToken: cfg.APIToken, JWTSecret: cfg.JWTSecret,
		JWTIssuer: cfg.JWTIssuer, JWTAudience: cfg.JWTAudience, AllowedOrigins: cfg.AllowedOrigins,
		TrustedProxies: cfg.TrustedProxies, RateLimitPerMinute: cfg.RateLimitPerMinute, RateLimiter: requestLimiter,
		WorkerMetrics:         workerMetrics,
		AllowOIDCFallbackAuth: cfg.AllowHostedBootstrapAuth,
		OIDCClient: httpcontroller.OIDCClientConfig{
			Issuer: cfg.OIDCIssuer, ClientID: cfg.OIDCCLIClientID, Audience: cfg.JWTAudience, Scope: cfg.OIDCCLIScope,
		},
		SiteConfig: httpcontroller.SiteConfig{TermsURL: cfg.TermsURL, PrivacyURL: cfg.PrivacyURL, SupportURL: cfg.SupportURL, AbuseEmail: cfg.AbuseEmail, SecurityEmail: cfg.SecurityEmail},
	}), close: closeFn}, nil
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
