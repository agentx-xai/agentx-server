package http

import (
	"agentx/server/internal/auth"
	"net/http"

	"github.com/gin-gonic/gin"
)

func registerMeRoute(r *gin.Engine) {
	r.GET("/v1/me", func(c *gin.Context) {
		principal, ok := auth.FromContext(c.Request.Context())
		if !ok {
			c.JSON(http.StatusOK, gin.H{"id": "anonymous", "subject": "anonymous"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": principal.UserID, "issuer": principal.Issuer, "subject": principal.Subject, "email": principal.Email})
	})
}
