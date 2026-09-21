// Package health регистрирует /healthz (liveness) и /readyz (readiness, проверяет зависимости).
package health

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Checker проверяет доступность одной зависимости (БД, MinIO, ...).
type Checker func(ctx context.Context) error

// Register монтирует /healthz и /readyz на engine. /healthz всегда 200, пока процесс жив.
// /readyz возвращает 200 только если все checkers отработали без ошибки.
func Register(engine *gin.Engine, checkers map[string]Checker) {
	engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	engine.GET("/readyz", func(c *gin.Context) {
		ctx := c.Request.Context()
		failed := gin.H{}
		for name, check := range checkers {
			if err := check(ctx); err != nil {
				failed[name] = err.Error()
			}
		}
		if len(failed) > 0 {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready", "failed": failed})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
}
