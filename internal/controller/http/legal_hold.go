package http

import (
	"agentx/server/internal/repo"
	legalholdusecase "agentx/server/internal/usecase/legalhold"
	"net/http"

	"github.com/gin-gonic/gin"
)

func registerLegalHoldRoutes(r *gin.Engine, service *legalholdusecase.Service) {
	r.GET("/v1/admin/legal-holds", func(c *gin.Context) {
		request, err := pageRequest(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_PAGINATION", err.Error()))
			return
		}
		page, err := service.List(c.Request.Context(), repo.LegalHoldFilter{Status: c.Query("status"), TargetType: c.Query("target_type"), TargetID: c.Query("target_id")}, request)
		if err != nil {
			writeServiceError(c, "LEGAL_HOLD_LIST_DENIED", err)
			return
		}
		writeRepositoryPage(c, request, page)
	})

	r.POST("/v1/admin/legal-holds", func(c *gin.Context) {
		var body struct {
			TargetType string `json:"target_type"`
			TargetID   string `json:"target_id"`
			Reason     string `json:"reason"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_REQUEST", "valid JSON body is required"))
			return
		}
		hold, err := service.Create(c.Request.Context(), body.TargetType, body.TargetID, body.Reason)
		if err != nil {
			writeServiceError(c, "LEGAL_HOLD_CREATE_DENIED", err)
			return
		}
		c.JSON(http.StatusCreated, hold)
	})

	r.POST("/v1/admin/legal-holds/:id/release", func(c *gin.Context) {
		var body struct {
			Confirmation string `json:"confirmation"`
			Reason       string `json:"reason"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, errorEnvelope(c, "INVALID_REQUEST", "valid JSON body is required"))
			return
		}
		hold, err := service.Release(c.Request.Context(), c.Param("id"), body.Confirmation, body.Reason)
		if err != nil {
			writeServiceError(c, "LEGAL_HOLD_RELEASE_DENIED", err)
			return
		}
		c.JSON(http.StatusOK, hold)
	})
}
