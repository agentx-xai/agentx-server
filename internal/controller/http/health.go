package http

import (
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
)

func registerHealth(r *gin.Engine, ready ...func(context.Context) error) {
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "agentx-api"}) })
	r.GET("/readyz", func(c *gin.Context) {
		if len(ready) > 0 && ready[0] != nil {
			if err := ready[0](c); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready", "error": err.Error()})
				return
			}
		}
		c.Status(http.StatusOK)
	})
}
