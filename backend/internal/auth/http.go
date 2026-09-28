package auth

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/httpx"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/transport/http/gen"
)

// Handler реализует операции auth из gen.ServerInterface (Login, GetCurrentUser).
type Handler struct {
	service *Service
	repo    *Repository
}

func NewHandler(service *Service, repo *Repository) *Handler {
	return &Handler{service: service, repo: repo}
}

func (h *Handler) Login(c *gin.Context) {
	var req gen.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, httpx.NewError(httpx.CodeValidationFailed, http.StatusBadRequest, "невалидное тело запроса"))
		return
	}

	token, ttl, _, err := h.service.Login(c.Request.Context(), req.Login, req.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			httpx.Fail(c, httpx.NewError(httpx.CodeUnauthorized, http.StatusUnauthorized, "неверный логин или пароль"))
			return
		}
		httpx.Fail(c, httpx.ErrInternal)
		return
	}

	c.JSON(http.StatusOK, gen.LoginResponse{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int(ttl.Seconds()),
	})
}

func (h *Handler) GetCurrentUser(c *gin.Context) {
	claims, ok := Require(c)
	if !ok {
		return
	}

	userID, err := claims.UserID()
	if err != nil {
		httpx.Fail(c, httpx.NewError(httpx.CodeUnauthorized, http.StatusUnauthorized, "невалидный токен"))
		return
	}

	u, err := h.repo.FindByID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Fail(c, httpx.NewError(httpx.CodeNotFound, http.StatusNotFound, "пользователь не найден"))
			return
		}
		httpx.Fail(c, httpx.ErrInternal)
		return
	}

	c.JSON(http.StatusOK, gen.User{
		Id:       u.ID,
		Login:    u.Login,
		FullName: u.FullName,
		Role:     gen.Role(u.Role),
		IsActive: u.IsActive,
	})
}
