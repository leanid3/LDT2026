package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORS разрешает браузеру страницы фронтенда (другой origin/порт) вызывать API. Авторизация — заголовок
// Authorization: Bearer, куки не используются, поэтому Access-Control-Allow-Credentials не выставляем и
// разрешение "*" безопасно (но для стенда лучше перечислить origin'ы явно).
//
// Origin, которого нет в списке, просто не получает CORS-заголовков — браузер сам заблокирует ответ;
// запрос при этом обрабатывается как обычно (CORS — защита браузера, не авторизация).
func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowAll := false
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		o = strings.TrimRight(strings.TrimSpace(o), "/")
		switch o {
		case "":
		case "*":
			allowAll = true
		default:
			allowed[o] = true
		}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" || (!allowAll && !allowed[origin]) {
			c.Next()
			return
		}

		h := c.Writer.Header()
		if allowAll {
			h.Set("Access-Control-Allow-Origin", "*")
		} else {
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
		}
		h.Set("Access-Control-Expose-Headers", "X-Request-ID")

		// Preflight: отвечаем сами и не пускаем дальше (для него нет маршрутов и авторизации).
		if c.Request.Method == http.MethodOptions && c.GetHeader("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Request-ID")
			h.Set("Access-Control-Max-Age", "600")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
