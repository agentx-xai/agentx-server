package http

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/usecase/workspace"
	"github.com/gin-gonic/gin"
	"net/http"
)

func registerWorkspaceRoutes(r *gin.Engine, s *workspace.Service) {
	r.GET("/v1/workspaces", func(c *gin.Context) {
		v, err := s.List(c)
		if err != nil {
			c.JSON(500, errorEnvelope(c, "WORKSPACE_LIST_FAILED", err.Error()))
			return
		}
		writeCollection(c, v)
	})
	r.POST("/v1/workspaces", func(c *gin.Context) {
		var req struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		}
		if c.ShouldBindJSON(&req) != nil {
			c.JSON(400, errorEnvelope(c, "INVALID_REQUEST", "invalid workspace"))
			return
		}
		v, err := s.Create(c, req.Name, req.Slug)
		if err != nil {
			c.JSON(400, errorEnvelope(c, "WORKSPACE_INVALID", err.Error()))
			return
		}
		c.JSON(http.StatusCreated, v)
	})
	r.GET("/v1/workspaces/:id/members", func(c *gin.Context) {
		v, err := s.Members(c, c.Param("id"))
		if err != nil {
			c.JSON(http.StatusForbidden, errorEnvelope(c, "WORKSPACE_ACCESS_DENIED", err.Error()))
			return
		}
		writeCollection(c, v)
	})
	r.POST("/v1/workspaces/:id/members", func(c *gin.Context) {
		var req struct {
			UserID string      `json:"user_id"`
			Role   entity.Role `json:"role"`
		}
		if c.ShouldBindJSON(&req) != nil {
			c.JSON(400, errorEnvelope(c, "INVALID_REQUEST", "invalid member"))
			return
		}
		if err := s.AddMember(c, c.Param("id"), req.UserID, req.Role); err != nil {
			c.JSON(http.StatusForbidden, errorEnvelope(c, "MEMBER_ADD_DENIED", err.Error()))
			return
		}
		c.Status(http.StatusCreated)
	})
	r.PATCH("/v1/workspaces/:id/members/:user_id", func(c *gin.Context) {
		var req struct {
			Role entity.Role `json:"role"`
		}
		if c.ShouldBindJSON(&req) != nil {
			c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_REQUEST", "invalid member role"))
			return
		}
		if err := s.UpdateMemberRole(c, c.Param("id"), c.Param("user_id"), req.Role); err != nil {
			c.JSON(http.StatusForbidden, errorEnvelope(c, "MEMBER_ROLE_UPDATE_DENIED", err.Error()))
			return
		}
		c.Status(http.StatusNoContent)
	})
	r.DELETE("/v1/workspaces/:id/members/:user_id", func(c *gin.Context) {
		if err := s.RemoveMember(c, c.Param("id"), c.Param("user_id")); err != nil {
			c.JSON(http.StatusForbidden, errorEnvelope(c, "MEMBER_REMOVE_DENIED", err.Error()))
			return
		}
		c.Status(http.StatusNoContent)
	})
	r.DELETE("/v1/workspaces/:id", func(c *gin.Context) {
		if err := s.Delete(c, c.Param("id")); err != nil {
			c.JSON(http.StatusForbidden, errorEnvelope(c, "WORKSPACE_DELETE_DENIED", err.Error()))
			return
		}
		c.Status(http.StatusNoContent)
	})
	_ = entity.RoleViewer
}
func errorEnvelope(c *gin.Context, code, message string) gin.H {
	return gin.H{"error": gin.H{"code": code, "message": message, "request_id": c.Writer.Header().Get("X-Request-ID")}}
}
