package http

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/usecase/workspace"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

func registerWorkspaceRoutes(r *gin.Engine, s *workspace.Service) {
	r.GET("/v1/invitations", func(c *gin.Context) {
		request, err := pageRequest(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_PAGINATION", err.Error()))
			return
		}
		invitations, err := s.MyInvitations(c, request)
		if err != nil {
			writeServiceError(c, "INVITATION_LIST_DENIED", err)
			return
		}
		writeRepositoryPage(c, request, invitations)
	})
	r.POST("/v1/invitations/:invitation_id/claim", func(c *gin.Context) {
		invitation, err := s.ClaimInvitation(c, c.Param("invitation_id"))
		if err != nil {
			writeServiceError(c, "INVITATION_CLAIM_DENIED", err)
			return
		}
		c.JSON(http.StatusOK, invitation)
	})
	r.GET("/v1/workspaces", func(c *gin.Context) {
		request, err := pageRequest(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_PAGINATION", err.Error()))
			return
		}
		v, err := s.ListPage(c, request)
		if err != nil {
			writeServiceError(c, "WORKSPACE_LIST_FAILED", err)
			return
		}
		writeRepositoryPage(c, request, v)
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
			writeServiceError(c, "WORKSPACE_INVALID", err)
			return
		}
		c.JSON(http.StatusCreated, v)
	})
	r.GET("/v1/workspaces/:id/members", func(c *gin.Context) {
		request, err := pageRequest(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_PAGINATION", err.Error()))
			return
		}
		v, err := s.MembersPage(c, c.Param("id"), request)
		if err != nil {
			writeServiceError(c, "WORKSPACE_ACCESS_DENIED", err)
			return
		}
		writeRepositoryPage(c, request, v)
	})
	r.GET("/v1/workspaces/:id/invitations", func(c *gin.Context) {
		request, err := pageRequest(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_PAGINATION", err.Error()))
			return
		}
		invitations, err := s.Invitations(c, c.Param("id"), request)
		if err != nil {
			writeServiceError(c, "INVITATION_LIST_DENIED", err)
			return
		}
		writeRepositoryPage(c, request, invitations)
	})
	r.POST("/v1/workspaces/:id/invitations", func(c *gin.Context) {
		var req struct {
			Email            string      `json:"email"`
			Role             entity.Role `json:"role"`
			ExpiresInSeconds int64       `json:"expires_in_seconds"`
		}
		if c.ShouldBindJSON(&req) != nil {
			c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_REQUEST", "invalid invitation"))
			return
		}
		expiresIn := time.Duration(0)
		if req.ExpiresInSeconds != 0 {
			if req.ExpiresInSeconds < 0 || req.ExpiresInSeconds > int64((31*24*time.Hour)/time.Second) {
				expiresIn = 31 * 24 * time.Hour
			} else {
				expiresIn = time.Duration(req.ExpiresInSeconds) * time.Second
			}
		}
		invitation, err := s.CreateInvitation(c, c.Param("id"), req.Email, req.Role, expiresIn)
		if err != nil {
			writeServiceError(c, "INVITATION_CREATE_DENIED", err)
			return
		}
		c.JSON(http.StatusCreated, invitation)
	})
	r.DELETE("/v1/workspaces/:id/invitations/:invitation_id", func(c *gin.Context) {
		if _, err := s.RevokeInvitation(c, c.Param("id"), c.Param("invitation_id")); err != nil {
			writeServiceError(c, "INVITATION_REVOKE_DENIED", err)
			return
		}
		c.Status(http.StatusNoContent)
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
			writeServiceError(c, "MEMBER_ROLE_UPDATE_DENIED", err)
			return
		}
		c.Status(http.StatusNoContent)
	})
	r.DELETE("/v1/workspaces/:id/members/:user_id", func(c *gin.Context) {
		if err := s.RemoveMember(c, c.Param("id"), c.Param("user_id")); err != nil {
			writeServiceError(c, "MEMBER_REMOVE_DENIED", err)
			return
		}
		c.Status(http.StatusNoContent)
	})
	r.DELETE("/v1/workspaces/:id", func(c *gin.Context) {
		if err := s.Delete(c, c.Param("id")); err != nil {
			writeServiceError(c, "WORKSPACE_DELETE_DENIED", err)
			return
		}
		c.Status(http.StatusNoContent)
	})
}
func errorEnvelope(c *gin.Context, code, message string) gin.H {
	return gin.H{"error": gin.H{"code": code, "message": message, "request_id": c.Writer.Header().Get("X-Request-ID")}}
}
