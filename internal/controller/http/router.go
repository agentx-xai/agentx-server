package http

import (
	"agentx/server/internal/auth"
	"agentx/server/internal/job"
	accountusecase "agentx/server/internal/usecase/account"
	"agentx/server/internal/usecase/device"
	driftusecase "agentx/server/internal/usecase/drift"
	legalholdusecase "agentx/server/internal/usecase/legalhold"
	manifestusecase "agentx/server/internal/usecase/manifest"
	policyusecase "agentx/server/internal/usecase/policy"
	reconcileusecase "agentx/server/internal/usecase/reconcile"
	"agentx/server/internal/usecase/registry"
	workspaceusecase "agentx/server/internal/usecase/workspace"
	"context"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type RouterOptions struct {
	Account               *accountusecase.Service
	Workspace             *workspaceusecase.Service
	Drift                 *driftusecase.Service
	Policy                *policyusecase.Service
	Manifest              *manifestusecase.Service
	LegalHold             *legalholdusecase.Service
	Reconcile             *reconcileusecase.Service
	OIDC                  *auth.OIDCVerifier
	Ready                 func(context.Context) error
	AllowLegacyUnscoped   bool
	APIToken              string
	JWTSecret             string
	JWTIssuer             string
	JWTAudience           string
	AllowedOrigins        []string
	TrustedProxies        []string
	RateLimitPerMinute    int
	RateLimiter           RequestRateLimiter
	WorkerMetrics         *job.Metrics
	Logger                *slog.Logger
	AllowOIDCFallbackAuth bool
	OIDCClient            OIDCClientConfig
	SiteConfig            SiteConfig
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
	r.ContextWithFallback = true
	logger := option.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if err := r.SetTrustedProxies(option.TrustedProxies); err != nil {
		logger.Error("invalid trusted proxy configuration", "error", err)
		_ = r.SetTrustedProxies(nil)
	}
	r.Use(middleware(option.AllowedOrigins, logger), recoveryMiddleware(logger), metricsMiddleware())
	requestLimiter := option.RateLimiter
	if requestLimiter == nil {
		requestLimiter = newRateLimiter(option.RateLimitPerMinute)
	}
	r.Use(rateLimitMiddleware(requestLimiter))
	if option.APIToken != "" || option.JWTSecret != "" || option.OIDC != nil {
		r.Use(tokenAuth(option.APIToken, option.JWTSecret, option.JWTIssuer, option.JWTAudience, option.OIDC, option.AllowOIDCFallbackAuth))
	}
	registerHealth(r, option.Ready)
	registerMetricsRoute(r, option.WorkerMetrics)
	registerOIDCClientConfigRoute(r, option.OIDCClient)
	registerSiteConfigRoute(r, option.SiteConfig)
	registerMeRoute(r)
	if option.Account != nil {
		registerAccountRoutes(r, option.Account)
	}
	if option.LegalHold != nil {
		registerLegalHoldRoutes(r, option.LegalHold)
	}
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
func middleware(origins []string, logger *slog.Logger) gin.HandlerFunc {
	if len(origins) == 0 {
		origins = []string{"*"}
	}
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		allowed[origin] = struct{}{}
	}
	return func(c *gin.Context) {
		started := time.Now()
		c.Set("agentx.logger", logger)
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		c.Header("X-Request-ID", id)
		requestContext := otel.GetTextMapPropagator().Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))
		requestContext, span := otel.Tracer("agentx/server/http").Start(requestContext, c.Request.Method+" request", trace.WithSpanKind(trace.SpanKindServer))
		c.Request = c.Request.WithContext(auth.WithRequestID(requestContext, id))
		defer func() {
			route := c.FullPath()
			if route == "" {
				route = "unmatched"
			}
			actorID := ""
			if principal, ok := auth.FromContext(c.Request.Context()); ok {
				actorID = principal.UserID
			}
			workspaceID := ""
			if len(c.Params) > 0 && strings.HasPrefix(route, "/v1/workspaces/:id") {
				workspaceID = c.Param("id")
			}
			span.SetName(c.Request.Method + " " + route)
			span.SetAttributes(attribute.String("http.request.method", c.Request.Method), attribute.String("http.route", route), attribute.Int("http.response.status_code", c.Writer.Status()), attribute.String("agentx.actor.id", actorID), attribute.String("agentx.workspace.id", workspaceID))
			if c.Writer.Status() >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, "server error")
			}
			span.End()
			logger.InfoContext(c.Request.Context(), "http_request", "request_id", id, "method", c.Request.Method, "route", route, "status", c.Writer.Status(), "duration_ms", time.Since(started).Milliseconds(), "actor_id", actorID, "workspace_id", workspaceID)
		}()
		origin := c.GetHeader("Origin")
		_, wildcard := allowed["*"]
		_, originAllowed := allowed[origin]
		if wildcard {
			c.Header("Access-Control-Allow-Origin", "*")
		} else if origin != "" && originAllowed {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID, Idempotency-Key")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
		if c.Request.Method == http.MethodOptions {
			if !wildcard && !originAllowed {
				c.AbortWithStatusJSON(http.StatusForbidden, errorEnvelope(c, "CORS_ORIGIN_DENIED", "origin is not allowed"))
				return
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func recoveryMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		logger.ErrorContext(c.Request.Context(), "request panic recovered", "request_id", auth.RequestID(c.Request.Context()), "panic", recovered)
		c.AbortWithStatusJSON(http.StatusInternalServerError, errorEnvelope(c, "INTERNAL_ERROR", "request could not be completed"))
	})
}

func tokenAuth(token, jwtSecret, issuer, audience string, oidcVerifier *auth.OIDCVerifier, allowOIDCFallback bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/healthz" || c.Request.URL.Path == "/readyz" || c.Request.URL.Path == "/metrics" || c.Request.URL.Path == "/v1/auth/config" || c.Request.URL.Path == "/v1/site/config" {
			c.Next()
			return
		}
		h := c.GetHeader("Authorization")
		if len(h) < 8 || h[:7] != "Bearer " {
			c.AbortWithStatusJSON(http.StatusUnauthorized, errorEnvelope(c, "UNAUTHORIZED", "valid bearer token required"))
			return
		}
		var p auth.Principal
		var err error
		if oidcVerifier != nil {
			p, err = oidcVerifier.Verify(c, h[7:])
			if err != nil && allowOIDCFallback {
				p, err = auth.VerifyBearer(h[7:], token, jwtSecret, issuer, audience)
			}
		} else {
			p, err = auth.VerifyBearer(h[7:], token, jwtSecret, issuer, audience)
		}
		if err != nil {
			requestLogger(c).WarnContext(c.Request.Context(), "bearer token rejected", "request_id", auth.RequestID(c.Request.Context()), "error", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, errorEnvelope(c, "UNAUTHORIZED", "valid bearer token required"))
			return
		}
		c.Request = c.Request.WithContext(auth.WithPrincipal(c.Request.Context(), p))
		c.Next()
	}
}
