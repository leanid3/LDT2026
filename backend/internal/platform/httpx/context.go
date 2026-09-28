package httpx

import "github.com/gin-gonic/gin"

// requestIDKey должен совпадать с middleware.RequestIDKey; дублируем строкой, а не импортом, чтобы
// избежать цикла (middleware уже импортирует httpx для ErrorResponse).
const requestIDKey = "request_id"

// RequestID достаёт request_id, положенный middleware.RequestID(), для использования в ErrorResponse
// доменными хендлерами.
func RequestID(c *gin.Context) string {
	return c.GetString(requestIDKey)
}

// Fail — короткий помощник: пишет ErrorResponse(err, RequestID(c)) в ответ.
func Fail(c *gin.Context, err *AppError) {
	status, body := ErrorResponse(err, RequestID(c))
	c.AbortWithStatusJSON(status, body)
}
