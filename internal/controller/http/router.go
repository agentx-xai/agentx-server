package http

import (
	"agentx/server/internal/auth"
	"agentx/server/internal/usecase/device"
	driftusecase "agentx/server/internal/usecase/drift"
	manifestusecase "agentx/server/internal/usecase/manifest"
	policyusecase "agentx/server/internal/usecase/policy"
	reconcileusecase "agentx/server/internal/usecase/reconcile"
	"agentx/server/internal/usecase/registry"
	workspaceusecase "agentx/server/internal/usecase/workspace"
	"context"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"
)

type RouterOptions struct {
	Workspace           *workspaceusecase.Service
	Drift               *driftusecase.Service
	Policy              *policyusecase.Service
	Manifest            *manifestusecase.Service
	Reconcile           *reconcileusecase.Service
	OIDC                *auth.OIDCVerifier
	Ready               func(context.Context) error
	AllowLegacyUnscoped bool
}

func NewRouter(ds *device.Service, rs *registry.Service, options ...RouterOptions) *gin.Engine {
	var option RouterOptions
	if len(options) == 0 {
		option.AllowLegacyUnscoped = true
	} else {
		option = options[0]
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), middleware(), metricsMiddleware())
	limit, _ := strconv.Atoi(os.Getenv("AGENTX_RATE_LIMIT_PER_MINUTE"))
	if limit == 0 && os.Getenv("AGENTX_RATE_LIMIT_PER_MINUTE") == "" {
		limit = 600
	}
	r.Use(newRateLimiter(limit).middleware())
	if token := os.Getenv("AGENTX_API_TOKEN"); token != "" || os.Getenv("AGENTX_JWT_SECRET") != "" || os.Getenv("AGENTX_OIDC_ISSUER") != "" {
		r.Use(tokenAuth(token, os.Getenv("AGENTX_JWT_SECRET"), os.Getenv("AGENTX_JWT_ISSUER"), os.Getenv("AGENTX_JWT_AUDIENCE"), option.OIDC))
	}
	registerHealth(r, option.Ready)
	registerMetricsRoute(r)
	registerMeRoute(r)
	registerDeviceRoutes(r, ds, option.Workspace, option.AllowLegacyUnscoped)
	registerAuditRoutes(r, ds, option.Workspace, option.AllowLegacyUnscoped)
	registerRegistryRoutes(r, rs, option.Workspace, option.AllowLegacyUnscoped)
	if option.Workspace != nil {
		registerWorkspaceRoutes(r, option.Workspace)
	}
	if option.Drift != nil {
		registerDriftRoutes(r, option.Drift, option.Workspace, option.AllowLegacyUnscoped)
	}
	if option.Policy != nil {
		registerPolicyRoutes(r, option.Policy)
	}
	if option.Manifest != nil {
		registerManifestRoutes(r, option.Manifest)
	}
	if option.Reconcile != nil {
		registerReconcileRoutes(r, option.Reconcile, option.Workspace)
	}
	return r
}
func middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		c.Header("X-Request-ID", id)
		c.Request = c.Request.WithContext(auth.WithRequestID(c.Request.Context(), id))
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID, Idempotency-Key")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
		slog.Default().Info("http_request", "request_id", id, "method", c.Request.Method, "path", c.Request.URL.Path, "status", c.Writer.Status(), "duration_ms", time.Since(started).Milliseconds())
	}
}
func tokenAuth(token, jwtSecret, issuer, audience string, oidcVerifier *auth.OIDCVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/healthz" || c.Request.URL.Path == "/readyz" {
			c.Next()
			return
		}
		h := c.GetHeader("Authorization")
		if len(h) < 8 || h[:7] != "Bearer " {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": "valid bearer token required"}})
			return
		}
		var p auth.Principal
		var err error
		if oidcVerifier != nil {
			p, err = oidcVerifier.Verify(c, h[7:])
		}
		if err != nil || oidcVerifier == nil {
			p, err = auth.VerifyBearer(h[7:], token, jwtSecret, issuer, audience)
		}
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": err.Error()}})
			return
		}
		c.Request = c.Request.WithContext(auth.WithPrincipal(c.Request.Context(), p))
		c.Next()
	}
}
