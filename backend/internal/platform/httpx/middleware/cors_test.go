package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/httpx/middleware"
)

func newRouter(origins []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.CORS(origins))
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	return r
}

func do(r http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCORS_AllowedOriginGetsHeaders(t *testing.T) {
	w := do(newRouter([]string{"http://localhost:5173"}), http.MethodGet, "/ping", map[string]string{"Origin": "http://localhost:5173"})
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "http://localhost:5173", w.Header().Get("Access-Control-Allow-Origin"))
	require.Contains(t, w.Header().Get("Vary"), "Origin")
}

func TestCORS_UnknownOriginGetsNoHeaders(t *testing.T) {
	w := do(newRouter([]string{"http://localhost:5173"}), http.MethodGet, "/ping", map[string]string{"Origin": "http://evil.example"})
	require.Equal(t, http.StatusOK, w.Code, "запрос обрабатывается: CORS — защита браузера, не авторизация")
	require.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_PreflightIsAnsweredWithoutRoute(t *testing.T) {
	w := do(newRouter([]string{"http://localhost:5173"}), http.MethodOptions, "/api/v1/objects", map[string]string{
		"Origin": "http://localhost:5173", "Access-Control-Request-Method": "POST",
		"Access-Control-Request-Headers": "authorization,content-type",
	})
	require.Equal(t, http.StatusNoContent, w.Code)
	require.Contains(t, w.Header().Get("Access-Control-Allow-Methods"), "POST")
	require.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), "Authorization")
	require.Equal(t, "http://localhost:5173", w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_Wildcard(t *testing.T) {
	w := do(newRouter([]string{"*"}), http.MethodGet, "/ping", map[string]string{"Origin": "http://anything"})
	require.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_NoOriginNoHeaders(t *testing.T) {
	w := do(newRouter([]string{"*"}), http.MethodGet, "/ping", nil)
	require.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_TrailingSlashInConfigIsTolerated(t *testing.T) {
	w := do(newRouter([]string{" http://localhost:5173/ "}), http.MethodGet, "/ping", map[string]string{"Origin": "http://localhost:5173"})
	require.Equal(t, "http://localhost:5173", w.Header().Get("Access-Control-Allow-Origin"))
}
