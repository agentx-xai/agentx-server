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
			request, e := pageRequest(c)
			if e != nil {
				c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_PAGINATION", e.Error()))
				return
			}
			v, e := s.ListPage(c, request)
			if e != nil {
				writeServiceError(c, "PACKAGE_LIST_FAILED", e)
				return
			}
			writeRepositoryPage(c, request, v)
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
				writeServiceError(c, "PUBLISH_FAILED", e)
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
				writeServiceError(c, "PACKAGE_LIST_FAILED", err)
				return
			}
			writeRepositoryPage(c, request, v)
		})
		r.GET("/v1/workspaces/:id/artifacts/:digest", func(c *gin.Context) {
			if err := ws.Authorize(c, c.Param("id"), entity.RoleViewer); err != nil {
				writeServiceError(c, "WORKSPACE_ACCESS_DENIED", err)
				return
			}
			f, err := s.OpenForWorkspace(c, c.Param("id"), c.Param("digest"))
			if err != nil {
				writeServiceError(c, "ARTIFACT_NOT_FOUND", err)
				return
			}
			defer f.Close()
			c.Header("Cache-Control", "private, max-age=31536000, immutable")
			c.DataFromReader(http.StatusOK, -1, "application/octet-stream", f, nil)
		})
		r.GET("/v1/workspaces/:id/packages/:name/:version/download", func(c *gin.Context) {
			if err := ws.Authorize(c, c.Param("id"), entity.RoleViewer); err != nil {
				writeServiceError(c, "WORKSPACE_ACCESS_DENIED", err)
				return
			}
			f, err := s.OpenReleaseForWorkspace(c, c.Param("id"), c.Param("name"), c.Param("version"))
			if err != nil {
				writeServiceError(c, "RELEASE_NOT_FOUND", err)
				return
			}
			defer f.Close()
			c.Header("Cache-Control", "private, max-age=31536000, immutable")
			c.DataFromReader(http.StatusOK, -1, "application/octet-stream", f, nil)
		})
		r.POST("/v1/workspaces/:id/packages/:name/releases", func(c *gin.Context) {
			if err := ws.Authorize(c, c.Param("id"), entity.RoleDeveloper); err != nil {
				writeServiceError(c, "PUBLISH_DENIED", err)
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
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 51<<20)
			f, _, err := c.Request.FormFile("artifact")
			if err != nil {
				c.JSON(400, errorEnvelope(c, "ARTIFACT_REQUIRED", "artifact is required"))
				return
			}
			defer f.Close()
			v, replayed, err := s.PublishForWorkspaceIdempotent(c, c.Param("id"), idempotencyKey, c.Param("name"), c.PostForm("version"), f, c.PostForm("signature"))
			if err != nil {
				writeServiceError(c, "PUBLISH_FAILED", err)
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
				writeServiceError(c, "APPROVAL_DENIED", err)
				return
			}
			c.JSON(http.StatusOK, v)
		})
	}
}
