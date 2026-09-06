package http

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/usecase/device"
	"agentx/server/internal/usecase/workspace"
	"github.com/gin-gonic/gin"
	"net/http"
)

func registerAuditRoutes(r *gin.Engine, s *device.Service, ws *workspace.Service, allowLegacy bool) {
	if allowLegacy {
		r.GET("/v1/audit-events", func(c *gin.Context) {
			v, e := s.Audit(c)
			if e != nil {
				c.JSON(http.StatusInternalServerError, errorEnvelope(c, "AUDIT_LIST_FAILED", e.Error()))
				return
			}
			writeCollection(c, v)
		})
	}
	if ws != nil {
		r.GET("/v1/workspaces/:id/audit-events", func(c *gin.Context) {
			if err := ws.Authorize(c, c.Param("id"), entity.RoleViewer); err != nil {
				c.JSON(http.StatusForbidden, errorEnvelope(c, "WORKSPACE_ACCESS_DENIED", err.Error()))
				return
			}
			v, err := s.AuditForWorkspace(c, c.Param("id"))
			if err != nil {
				c.JSON(500, errorEnvelope(c, "AUDIT_LIST_FAILED", err.Error()))
				return
			}
			writeCollection(c, v)
		})
	}
}
