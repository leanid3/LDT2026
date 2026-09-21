package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// Logger логирует завершённые запросы в формате logging.New (JSON stdout).
func Logger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		status := c.Writer.Status()
		args := []any{
			"request_id", c.GetString(RequestIDKey),
			"method", c.Request.Method,
			"path", path,
			"status", status,
			"latency_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
		}
		if query != "" {
			args = append(args, "query", query)
		}

		switch {
		case status >= 500:
			log.Error("request completed", args...)
		case status >= 400:
			log.Warn("request completed", args...)
		default:
			log.Info("request completed", args...)
		}
	}
}
