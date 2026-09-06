package http

import (
	"agentx/server/internal/usecase/policy"
	"net/http"

	"github.com/gin-gonic/gin"
)

func registerPolicyRoutes(r *gin.Engine, s *policy.Service) {
	r.GET("/v1/workspaces/:id/policies", func(c *gin.Context) {
		current, err := s.Current(c, c.Param("id"))
		if err != nil {
			c.JSON(http.StatusForbidden, errorEnvelope(c, "POLICY_READ_DENIED", err.Error()))
			return
		}
		c.JSON(http.StatusOK, current)
	})
	r.PUT("/v1/workspaces/:id/policies", func(c *gin.Context) {
		var payload map[string]any
		if err := c.ShouldBindJSON(&payload); err != nil {
			c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_REQUEST", "invalid policy document"))
			return
		}
		document := payload
		if nested, ok := payload["document"].(map[string]any); ok {
			document = nested
		}
		current, err := s.Replace(c, c.Param("id"), document)
		if err != nil {
			status := http.StatusForbidden
			if err.Error() == "policy document is required" {
				status = http.StatusBadRequest
			}
			c.JSON(status, errorEnvelope(c, "POLICY_UPDATE_FAILED", err.Error()))
			return
		}
		c.JSON(http.StatusOK, current)
	})
}
