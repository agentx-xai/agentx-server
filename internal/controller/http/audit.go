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
			request, e := pageRequest(c)
			if e != nil {
				c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_PAGINATION", e.Error()))
				return
			}
			v, e := s.AuditPage(c, request)
			if e != nil {
				writeServiceError(c, "AUDIT_LIST_FAILED", e)
				return
			}
			writeRepositoryPage(c, request, v)
		})
	}
	if ws != nil {
		r.GET("/v1/workspaces/:id/audit-events", func(c *gin.Context) {
			if err := ws.Authorize(c, c.Param("id"), entity.RoleViewer); err != nil {
				writeServiceError(c, "WORKSPACE_ACCESS_DENIED", err)
				return
			}
			request, err := pageRequest(c)
			if err != nil {
				c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_PAGINATION", err.Error()))
				return
			}
			v, err := s.AuditForWorkspacePage(c, c.Param("id"), request)
			if err != nil {
				writeServiceError(c, "AUDIT_LIST_FAILED", err)
				return
			}
			writeRepositoryPage(c, request, v)
		})
	}
}
