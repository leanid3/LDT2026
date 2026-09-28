package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/httpx"
)

const claimsContextKey = "auth_claims"

// AttachClaims — глобальный middleware (навешивается на весь engine, как RequestID/Recovery/Logger):
// если заголовок Authorization: Bearer присутствует и валиден, кладёт Claims в контекст. Не отклоняет
// запрос сам — публичные операции (/healthz, /readyz, /auth/login) не должны требовать токен;
// проверку обязательности делают конкретные хендлеры через Require/RequireRole ниже.
func AttachClaims(issuer *Issuer) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) {
			c.Next()
			return
		}

		claims, err := issuer.Parse(strings.TrimPrefix(header, prefix))
		if err != nil {
			c.Next()
			return
		}
		c.Set(claimsContextKey, claims)
		c.Next()
	}
}

func FromContext(c *gin.Context) (*Claims, bool) {
	v, ok := c.Get(claimsContextKey)
	if !ok {
		return nil, false
	}
	claims, ok := v.(*Claims)
	return claims, ok
}

// Require — вызывается в начале защищённого хендлера. При отсутствии/невалидности токена сам пишет
// 401 и возвращает ok=false — хендлер должен просто return.
func Require(c *gin.Context) (*Claims, bool) {
	claims, ok := FromContext(c)
	if !ok {
		httpx.Fail(c, httpx.NewError(httpx.CodeUnauthorized, http.StatusUnauthorized, "требуется авторизация"))
		return nil, false
	}
	return claims, true
}

// RequireRole — как Require, но дополнительно проверяет роль. Пишет 403, если роль не подходит.
func RequireRole(c *gin.Context, roles ...string) (*Claims, bool) {
	claims, ok := Require(c)
	if !ok {
		return nil, false
	}
	for _, r := range roles {
		if claims.Role == r {
			return claims, true
		}
	}
	httpx.Fail(c, httpx.NewError(httpx.CodeForbidden, http.StatusForbidden, "недостаточно прав"))
	return nil, false
}
