package http

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/usecase/reconcile"
	"agentx/server/internal/usecase/workspace"
	"github.com/gin-gonic/gin"
	"net/http"
)

func registerReconcileRoutes(r *gin.Engine, s *reconcile.Service, ws *workspace.Service) {
	if s == nil || ws == nil {
		return
	}
	r.GET("/v1/workspaces/:id/devices/:device_id/plan", func(c *gin.Context) {
		if err := ws.Authorize(c, c.Param("id"), entity.RoleViewer); err != nil {
			writeServiceError(c, "WORKSPACE_ACCESS_DENIED", err)
			return
		}
		plan, err := s.Plan(c, c.Param("id"), c.Param("device_id"))
		if err != nil {
			writeServiceError(c, "RECONCILE_PLAN_FAILED", err)
			return
		}
		c.JSON(http.StatusOK, plan)
	})
}
