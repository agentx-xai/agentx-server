package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type OIDCClientConfig struct {
	Issuer   string `json:"issuer"`
	ClientID string `json:"client_id"`
	Audience string `json:"audience,omitempty"`
	Scope    string `json:"scope"`
}

func registerOIDCClientConfigRoute(r *gin.Engine, config OIDCClientConfig) {
	r.GET("/v1/auth/config", func(c *gin.Context) {
		if config.Issuer == "" || config.ClientID == "" {
			c.JSON(http.StatusNotFound, errorEnvelope(c, "OIDC_NOT_CONFIGURED", "OIDC device login is not configured"))
			return
		}
		c.JSON(http.StatusOK, config)
	})
}
