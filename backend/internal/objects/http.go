package objects

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/auth"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/httpx"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/transport/http/gen"
)

type Handler struct {
	repo *Repository
}

func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

func (h *Handler) CreateObject(c *gin.Context) {
	if _, ok := auth.Require(c); !ok {
		return
	}

	var req gen.ObjectCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		httpx.Fail(c, httpx.NewError(httpx.CodeValidationFailed, http.StatusBadRequest, "поле name обязательно"))
		return
	}

	created, err := h.repo.Create(c.Request.Context(), Object{
		ID: uuid.New(), ExternalID: req.ExternalId, Name: req.Name, Address: req.Address,
		Customer: req.Customer, Contractor: req.Contractor, PermitNumber: req.PermitNumber,
	})
	if err != nil {
		httpx.Fail(c, httpx.ErrInternal)
		return
	}

	c.JSON(http.StatusCreated, toAPI(created))
}

func (h *Handler) ListObjects(c *gin.Context) {
	if _, ok := auth.Require(c); !ok {
		return
	}

	list, err := h.repo.List(c.Request.Context())
	if err != nil {
		httpx.Fail(c, httpx.ErrInternal)
		return
	}

	items := make([]gen.Object, 0, len(list))
	for _, o := range list {
		items = append(items, toAPI(o))
	}
	c.JSON(http.StatusOK, gen.ObjectList{Items: items})
}

func (h *Handler) GetObject(c *gin.Context, id gen.IdParam) {
	if _, ok := auth.Require(c); !ok {
		return
	}

	o, err := h.repo.Get(c.Request.Context(), uuid.UUID(id))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Fail(c, httpx.NewError(httpx.CodeNotFound, http.StatusNotFound, "объект не найден"))
			return
		}
		httpx.Fail(c, httpx.ErrInternal)
		return
	}
	c.JSON(http.StatusOK, toAPI(o))
}

func toAPI(o Object) gen.Object {
	return gen.Object{
		Id: o.ID, ExternalId: o.ExternalID, Name: o.Name, Address: o.Address,
		Customer: o.Customer, Contractor: o.Contractor, PermitNumber: o.PermitNumber, CreatedAt: o.CreatedAt,
	}
}
