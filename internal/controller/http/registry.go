package http

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/usecase/registry"
	"agentx/server/internal/usecase/workspace"
	"github.com/gin-gonic/gin"
	"net/http"
)

func registerRegistryRoutes(r *gin.Engine, s *registry.Service, ws *workspace.Service, allowLegacy bool) {
	if allowLegacy {
		r.GET("/v1/packages", func(c *gin.Context) {
			v, e := s.List(c)
			if e != nil {
				c.JSON(http.StatusInternalServerError, errorEnvelope(c, "PACKAGE_LIST_FAILED", e.Error()))
				return
			}
			writeCollection(c, v)
		})
		r.POST("/v1/packages/:name/releases", func(c *gin.Context) {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 51<<20)
			f, _, e := c.Request.FormFile("artifact")
			if e != nil {
				c.JSON(http.StatusBadRequest, errorEnvelope(c, "ARTIFACT_REQUIRED", "artifact is required"))
				return
			}
			defer f.Close()
			v, e := s.Publish(c, c.Param("name"), c.PostForm("version"), f, c.PostForm("signature"))
			if e != nil {
				c.JSON(http.StatusBadRequest, errorEnvelope(c, "PUBLISH_FAILED", e.Error()))
				return
			}
			c.JSON(201, v)
		})
		r.GET("/v1/artifacts/:digest", func(c *gin.Context) {
			f, e := s.Open(c, c.Param("digest"))
			if e != nil {
				c.JSON(http.StatusNotFound, errorEnvelope(c, "ARTIFACT_NOT_FOUND", "artifact not found"))
				return
			}
			defer f.Close()
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
			c.DataFromReader(200, -1, "application/octet-stream", f, nil)
		})
	}
	if ws != nil {
		r.GET("/v1/workspaces/:id/packages", func(c *gin.Context) {
			if err := ws.Authorize(c, c.Param("id"), entity.RoleViewer); err != nil {
				c.JSON(http.StatusForbidden, errorEnvelope(c, "WORKSPACE_ACCESS_DENIED", err.Error()))
				return
			}
			v, err := s.ListForWorkspace(c, c.Param("id"))
			if err != nil {
				c.JSON(500, errorEnvelope(c, "PACKAGE_LIST_FAILED", err.Error()))
				return
			}
			writeCollection(c, v)
		})
		r.GET("/v1/workspaces/:id/artifacts/:digest", func(c *gin.Context) {
			if err := ws.Authorize(c, c.Param("id"), entity.RoleViewer); err != nil {
				c.JSON(http.StatusForbidden, errorEnvelope(c, "WORKSPACE_ACCESS_DENIED", err.Error()))
				return
			}
			f, err := s.OpenForWorkspace(c, c.Param("id"), c.Param("digest"))
			if err != nil {
				c.JSON(http.StatusNotFound, errorEnvelope(c, "ARTIFACT_NOT_FOUND", "artifact not found"))
				return
			}
			defer f.Close()
			c.Header("Cache-Control", "private, max-age=31536000, immutable")
			c.DataFromReader(http.StatusOK, -1, "application/octet-stream", f, nil)
		})
		r.GET("/v1/workspaces/:id/packages/:name/:version/download", func(c *gin.Context) {
			if err := ws.Authorize(c, c.Param("id"), entity.RoleViewer); err != nil {
				c.JSON(http.StatusForbidden, errorEnvelope(c, "WORKSPACE_ACCESS_DENIED", err.Error()))
				return
			}
			f, err := s.OpenReleaseForWorkspace(c, c.Param("id"), c.Param("name"), c.Param("version"))
			if err != nil {
				c.JSON(http.StatusNotFound, errorEnvelope(c, "RELEASE_NOT_FOUND", "release is unavailable"))
				return
			}
			defer f.Close()
			c.Header("Cache-Control", "private, max-age=31536000, immutable")
			c.DataFromReader(http.StatusOK, -1, "application/octet-stream", f, nil)
		})
		r.POST("/v1/workspaces/:id/packages/:name/releases", func(c *gin.Context) {
			if err := ws.Authorize(c, c.Param("id"), entity.RoleDeveloper); err != nil {
				c.JSON(http.StatusForbidden, errorEnvelope(c, "PUBLISH_DENIED", err.Error()))
				return
			}
			idempotencyKey := c.GetHeader("Idempotency-Key")
			if len(idempotencyKey) > 255 {
				c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_IDEMPOTENCY_KEY", "Idempotency-Key must be at most 255 bytes"))
				return
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 51<<20)
			f, _, err := c.Request.FormFile("artifact")
			if err != nil {
				c.JSON(400, errorEnvelope(c, "ARTIFACT_REQUIRED", "artifact is required"))
				return
			}
			defer f.Close()
			v, replayed, err := s.PublishForWorkspaceIdempotent(c, c.Param("id"), idempotencyKey, c.Param("name"), c.PostForm("version"), f, c.PostForm("signature"))
			if err != nil {
				c.JSON(400, errorEnvelope(c, "PUBLISH_FAILED", err.Error()))
				return
			}
			if replayed {
				c.Header("X-Idempotent-Replay", "true")
			}
			c.JSON(http.StatusCreated, v)
		})
		r.POST("/v1/workspaces/:id/packages/:name/:version/approve", func(c *gin.Context) {
			v, err := s.ApproveForWorkspace(c, c.Param("id"), c.Param("name"), c.Param("version"))
			if err != nil {
				c.JSON(http.StatusForbidden, errorEnvelope(c, "APPROVAL_DENIED", err.Error()))
				return
			}
			c.JSON(http.StatusOK, v)
		})
	}
}
