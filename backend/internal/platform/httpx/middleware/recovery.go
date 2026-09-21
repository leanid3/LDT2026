package middleware

import (
	"log/slog"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/httpx"
)

// Recovery перехватывает панику в хендлере и отдаёт единый формат ошибки (backend-plan.md §7).
func Recovery(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("panic recovered",
					"error", r,
					"stack", string(debug.Stack()),
					"request_id", c.GetString(RequestIDKey),
					"path", c.Request.URL.Path,
					"method", c.Request.Method,
				)

				status, body := httpx.ErrorResponse(httpx.ErrInternal, c.GetString(RequestIDKey))
				c.AbortWithStatusJSON(status, body)
			}
		}()
		c.Next()
	}
}
