package http

import (
	"agentx/server/internal/apperror"
	accountusecase "agentx/server/internal/usecase/account"
	"net/http"

	"github.com/gin-gonic/gin"
)

func registerAccountRoutes(r *gin.Engine, service *accountusecase.Service) {
	r.GET("/v1/me/export", func(c *gin.Context) {
		export, err := service.Export(c.Request.Context())
		if err != nil {
			writeServiceError(c, "ACCOUNT_EXPORT_FAILED", err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Header("Content-Disposition", `attachment; filename="agentx-account-export.json"`)
		c.JSON(http.StatusOK, export)
	})

	r.DELETE("/v1/me", func(c *gin.Context) {
		var body struct {
			Confirmation string `json:"confirmation"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			writeServiceError(c, "ACCOUNT_DELETE_FAILED", apperror.New(apperror.KindValidation, "valid JSON body is required"))
			return
		}
		if err := service.Delete(c.Request.Context(), body.Confirmation); err != nil {
			writeServiceError(c, "ACCOUNT_DELETE_FAILED", err)
			return
		}
		c.Status(http.StatusNoContent)
	})
}
