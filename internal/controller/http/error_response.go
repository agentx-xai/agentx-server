package http

import (
	"agentx/server/internal/apperror"
	"agentx/server/internal/auth"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

func writeServiceError(c *gin.Context, code string, err error) {
	if kind, message, ok := apperror.Public(err); ok {
		status := http.StatusBadRequest
		switch kind {
		case apperror.KindForbidden:
			status = http.StatusForbidden
		case apperror.KindNotFound:
			status = http.StatusNotFound
		case apperror.KindConflict:
			status = http.StatusBadRequest
		}
		c.JSON(status, errorEnvelope(c, code, message))
		return
	}
	writeInternalError(c, code, err)
}

func writeInternalError(c *gin.Context, code string, err error) {
	requestLogger(c).ErrorContext(c.Request.Context(), "request failed", "request_id", auth.RequestID(c.Request.Context()), "error_code", code, "error", err)
	c.JSON(http.StatusInternalServerError, errorEnvelope(c, code, "request could not be completed"))
}

func requestLogger(c *gin.Context) *slog.Logger {
	if value, exists := c.Get("agentx.logger"); exists {
		if logger, ok := value.(*slog.Logger); ok && logger != nil {
			return logger
		}
	}
	return slog.Default()
}
