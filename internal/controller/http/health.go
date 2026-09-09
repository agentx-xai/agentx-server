package http

import (
	"agentx/server/internal/auth"
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
)

func registerHealth(r *gin.Engine, ready ...func(context.Context) error) {
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "agentx-api"}) })
	r.GET("/readyz", func(c *gin.Context) {
		if len(ready) > 0 && ready[0] != nil {
			if err := ready[0](c); err != nil {
				requestLogger(c).ErrorContext(c.Request.Context(), "readiness check failed", "request_id", auth.RequestID(c.Request.Context()), "error", err)
				c.JSON(http.StatusServiceUnavailable, errorEnvelope(c, "NOT_READY", "dependency unavailable"))
				return
			}
		}
		c.Status(http.StatusOK)
	})
}
