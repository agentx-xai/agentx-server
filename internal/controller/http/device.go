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
			v, e := s.List(c)
			if e != nil {
				c.JSON(http.StatusInternalServerError, errorEnvelope(c, "DEVICE_LIST_FAILED", e.Error()))
				return
			}
			writeCollection(c, v)
		})
		r.POST("/v1/devices", func(c *gin.Context) {
			var d entity.Device
			if c.ShouldBindJSON(&d) != nil {
				c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_REQUEST", "invalid device"))
				return
			}
			v, e := s.Register(c, d)
			if e != nil {
				c.JSON(http.StatusBadRequest, errorEnvelope(c, "DEVICE_INVALID", e.Error()))
				return
			}
			c.JSON(http.StatusCreated, v)
		})
		r.POST("/v1/devices/:id/heartbeat", func(c *gin.Context) {
			if e := s.Heartbeat(c, c.Param("id")); e != nil {
				c.JSON(http.StatusNotFound, errorEnvelope(c, "DEVICE_NOT_FOUND", e.Error()))
				return
			}
			c.Status(http.StatusNoContent)
		})
	}
	if ws != nil {
		r.GET("/v1/workspaces/:id/devices", func(c *gin.Context) {
			if err := ws.Authorize(c, c.Param("id"), entity.RoleViewer); err != nil {
				c.JSON(http.StatusForbidden, errorEnvelope(c, "WORKSPACE_ACCESS_DENIED", err.Error()))
				return
			}
			v, err := s.ListForWorkspace(c, c.Param("id"))
			if err != nil {
				c.JSON(500, errorEnvelope(c, "DEVICE_LIST_FAILED", err.Error()))
				return
			}
			writeCollection(c, v)
		})
		r.POST("/v1/workspaces/:id/devices", func(c *gin.Context) {
			if err := ws.Authorize(c, c.Param("id"), entity.RoleDeveloper); err != nil {
				c.JSON(http.StatusForbidden, errorEnvelope(c, "DEVICE_REGISTER_DENIED", err.Error()))
				return
			}
			var d entity.Device
			if c.ShouldBindJSON(&d) != nil {
				c.JSON(400, errorEnvelope(c, "INVALID_REQUEST", "invalid device"))
				return
			}
			v, err := s.RegisterForWorkspace(c, c.Param("id"), d)
			if err != nil {
				c.JSON(400, errorEnvelope(c, "DEVICE_INVALID", err.Error()))
				return
			}
			c.JSON(http.StatusCreated, v)
		})
		r.POST("/v1/workspaces/:id/devices/:device_id/heartbeat", func(c *gin.Context) {
			if err := ws.Authorize(c, c.Param("id"), entity.RoleDeveloper); err != nil {
				c.JSON(http.StatusForbidden, errorEnvelope(c, "DEVICE_HEARTBEAT_DENIED", err.Error()))
				return
			}
			var d entity.Device
			if err := c.ShouldBindJSON(&d); err != nil && err.Error() != "EOF" {
				c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_REQUEST", "invalid heartbeat"))
				return
			}
			if err := s.HeartbeatForWorkspace(c, c.Param("id"), c.Param("device_id"), d); err != nil {
				c.JSON(http.StatusNotFound, errorEnvelope(c, "DEVICE_NOT_FOUND", err.Error()))
				return
			}
			c.Status(http.StatusNoContent)
		})
	}
}
