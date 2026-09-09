package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type SiteConfig struct {
	TermsURL      string `json:"terms_url"`
	PrivacyURL    string `json:"privacy_url"`
	SupportURL    string `json:"support_url"`
	AbuseEmail    string `json:"abuse_email"`
	SecurityEmail string `json:"security_email"`
}

func registerSiteConfigRoute(router *gin.Engine, config SiteConfig) {
	router.GET("/v1/site/config", func(c *gin.Context) {
		if config.TermsURL == "" || config.PrivacyURL == "" || config.SupportURL == "" || config.AbuseEmail == "" || config.SecurityEmail == "" {
			c.JSON(http.StatusNotFound, errorEnvelope(c, "SITE_CONFIG_NOT_CONFIGURED", "site information is not configured"))
			return
		}
		c.JSON(http.StatusOK, config)
	})
}
