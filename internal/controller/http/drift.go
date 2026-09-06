package http

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/usecase/drift"
	"agentx/server/internal/usecase/workspace"
	"github.com/gin-gonic/gin"
	"net/http"
)

func registerDriftRoutes(r *gin.Engine, s *drift.Service, ws *workspace.Service, allowLegacy bool) {
	if allowLegacy {
		r.GET("/v1/drift", func(c *gin.Context) {
			v, err := s.List(c)
			if err != nil {
				c.JSON(500, errorEnvelope(c, "DRIFT_FAILED", err.Error()))
				return
			}
			writeDriftCollection(c, v)
		})
	}
	if ws != nil {
		r.GET("/v1/workspaces/:id/drift", func(c *gin.Context) {
			if err := ws.Authorize(c, c.Param("id"), entity.RoleViewer); err != nil {
				c.JSON(http.StatusForbidden, errorEnvelope(c, "WORKSPACE_ACCESS_DENIED", err.Error()))
				return
			}
			v, err := s.ListForWorkspace(c, c.Param("id"))
			if err != nil {
				c.JSON(500, errorEnvelope(c, "DRIFT_FAILED", err.Error()))
				return
			}
			writeDriftCollection(c, v)
		})
	}
}

func writeDriftCollection(c *gin.Context, items []entity.DriftItem) {
	limit, offset, paged, err := collectionPage(c, len(items))
	if err != nil {
		c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_PAGINATION", err.Error()))
		return
	}
	page, next := slicePage(items, limit, offset)
	if next != "" {
		c.Header("X-Next-Cursor", next)
	}
	if !paged {
		c.JSON(http.StatusOK, gin.H{"items": page, "count": len(items)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": page, "count": len(items), "next_cursor": next})
}
