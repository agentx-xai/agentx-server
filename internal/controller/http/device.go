package http

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/usecase/device"
	"agentx/server/internal/usecase/workspace"
	"github.com/gin-gonic/gin"
	"net/http"
)

func registerDeviceRoutes(r *gin.Engine, s *device.Service, ws *workspace.Service, allowLegacy bool) {
	if allowLegacy {
		r.GET("/v1/devices", func(c *gin.Context) {
			request, e := pageRequest(c)
			if e != nil {
				c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_PAGINATION", e.Error()))
				return
			}
			v, e := s.ListPage(c, request)
			if e != nil {
				writeServiceError(c, "DEVICE_LIST_FAILED", e)
				return
			}
			writeRepositoryPage(c, request, v)
		})
		r.POST("/v1/devices", func(c *gin.Context) {
			var d entity.Device
			if c.ShouldBindJSON(&d) != nil {
				c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_REQUEST", "invalid device"))
				return
			}
			v, e := s.Register(c, d)
			if e != nil {
				writeServiceError(c, "DEVICE_INVALID", e)
				return
			}
			c.JSON(http.StatusCreated, v)
		})
		r.POST("/v1/devices/:id/heartbeat", func(c *gin.Context) {
			if e := s.Heartbeat(c, c.Param("id")); e != nil {
				writeServiceError(c, "DEVICE_NOT_FOUND", e)
				return
			}
			c.Status(http.StatusNoContent)
		})
	}
	if ws != nil {
		r.GET("/v1/workspaces/:id/devices", func(c *gin.Context) {
			if err := ws.Authorize(c, c.Param("id"), entity.RoleViewer); err != nil {
				writeServiceError(c, "WORKSPACE_ACCESS_DENIED", err)
				return
			}
			request, err := pageRequest(c)
			if err != nil {
				c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_PAGINATION", err.Error()))
				return
			}
			v, err := s.ListForWorkspacePage(c, c.Param("id"), request)
			if err != nil {
				writeServiceError(c, "DEVICE_LIST_FAILED", err)
				return
			}
			writeRepositoryPage(c, request, v)
		})
		r.POST("/v1/workspaces/:id/devices", func(c *gin.Context) {
			if err := ws.Authorize(c, c.Param("id"), entity.RoleDeveloper); err != nil {
				writeServiceError(c, "DEVICE_REGISTER_DENIED", err)
				return
			}
			idempotencyKey := c.GetHeader("Idempotency-Key")
			if idempotencyKey == "" {
				c.JSON(http.StatusBadRequest, errorEnvelope(c, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key is required"))
				return
			}
			if len(idempotencyKey) > 255 {
				c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_IDEMPOTENCY_KEY", "Idempotency-Key must be at most 255 bytes"))
				return
			}
			var d entity.Device
			if c.ShouldBindJSON(&d) != nil {
				c.JSON(400, errorEnvelope(c, "INVALID_REQUEST", "invalid device"))
				return
			}
			v, replayed, err := s.RegisterForWorkspaceIdempotent(c, c.Param("id"), idempotencyKey, d)
			if err != nil {
				writeServiceError(c, "DEVICE_INVALID", err)
				return
			}
			if replayed {
				c.Header("X-Idempotent-Replay", "true")
			}
			c.JSON(http.StatusCreated, v)
		})
		r.POST("/v1/workspaces/:id/devices/:device_id/heartbeat", func(c *gin.Context) {
			if err := ws.Authorize(c, c.Param("id"), entity.RoleDeveloper); err != nil {
				writeServiceError(c, "DEVICE_HEARTBEAT_DENIED", err)
				return
			}
			var d entity.Device
			if err := c.ShouldBindJSON(&d); err != nil && err.Error() != "EOF" {
				c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_REQUEST", "invalid heartbeat"))
				return
			}
			if err := s.HeartbeatForWorkspace(c, c.Param("id"), c.Param("device_id"), d); err != nil {
				writeServiceError(c, "DEVICE_NOT_FOUND", err)
				return
			}
			c.Status(http.StatusNoContent)
		})
	}
}
