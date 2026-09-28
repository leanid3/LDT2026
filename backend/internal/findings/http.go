package findings

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
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetFinding(c *gin.Context, id gen.IdParam) {
	if _, ok := auth.Require(c); !ok {
		return
	}

	f, err := h.service.Get(c.Request.Context(), uuid.UUID(id))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Fail(c, httpx.NewError(httpx.CodeNotFound, http.StatusNotFound, "finding не найден"))
			return
		}
		httpx.Fail(c, httpx.ErrInternal)
		return
	}
	c.JSON(http.StatusOK, ToAPI(f))
}

func (h *Handler) DecideFinding(c *gin.Context, id gen.IdParam) {
	claims, ok := auth.Require(c)
	if !ok {
		return
	}
	actorID, err := claims.UserID()
	if err != nil {
		httpx.Fail(c, httpx.NewError(httpx.CodeUnauthorized, http.StatusUnauthorized, "невалидный токен"))
		return
	}

	var req gen.FindingDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, httpx.NewError(httpx.CodeValidationFailed, http.StatusBadRequest, "невалидное тело запроса"))
		return
	}

	d := Decision{Decision: string(req.Decision), Comment: req.Comment}
	if req.ReasonCode != nil {
		rc := string(*req.ReasonCode)
		d.ReasonCode = &rc
	}

	f, err := h.service.Decide(c.Request.Context(), uuid.UUID(id), actorID, d)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, ToAPI(f))
}

func writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrValidation):
		httpx.Fail(c, httpx.NewError(httpx.CodeValidationFailed, http.StatusBadRequest, err.Error()))
	case errors.Is(err, ErrInvalidTransition):
		httpx.Fail(c, httpx.NewError(httpx.CodeConflict, http.StatusConflict, err.Error()))
	case errors.Is(err, ErrNotFound):
		httpx.Fail(c, httpx.NewError(httpx.CodeNotFound, http.StatusNotFound, "finding не найден"))
	default:
		httpx.Fail(c, httpx.ErrInternal)
	}
}

// ToAPI — единственное место, где доменный Finding превращается в gen.Finding: и карточка
// (GET /findings/{id}), и протокол (GET /processes/{id}/protocol) обязаны отдавать одно и то же.
func ToAPI(f Finding) gen.Finding {
	out := gen.Finding{
		Id: f.ID, CheckId: f.CheckID, ParentFindingId: f.ParentFindingID,
		FindingStatus: gen.FindingStatus(f.FindingStatus), InspectorStatus: gen.InspectorStatus(f.InspectorStatus),
		DecidedBy: f.DecidedBy, DecidedAt: f.DecidedAt, Comment: f.Comment, Version: f.Version,
	}
	if f.ReasonCode != nil {
		rc := gen.ReasonCode(*f.ReasonCode)
		out.ReasonCode = &rc
	}

	out.ParamCode, out.ParameterName, out.Unit = f.ParamCode, f.ParameterName, f.Unit
	out.ExpectedValue, out.ActualValue, out.Delta, out.Rationale = f.ExpectedValue, f.ActualValue, f.Delta, f.Rationale
	if f.ReviewPriority != nil {
		rp := gen.FindingReviewPriority(*f.ReviewPriority)
		out.ReviewPriority = &rp
	}
	if len(f.Evidence) > 0 {
		ev := make([]gen.EvidenceFragment, 0, len(f.Evidence))
		for _, e := range f.Evidence {
			item := gen.EvidenceFragment{
				FileId: e.FileID, Page: e.Page, Quote: e.Quote, ExtractedValue: e.ExtractedValue,
				Role: gen.EvidenceFragmentRole(e.Role),
			}
			if e.OriginalName != "" {
				name := e.OriginalName
				item.OriginalName = &name
			}
			if e.Stage != nil {
				st := gen.DocStage(*e.Stage)
				item.Stage = &st
			}
			if len(e.BBox) == 4 {
				box := []float32{float32(e.BBox[0]), float32(e.BBox[1]), float32(e.BBox[2]), float32(e.BBox[3])}
				item.Bbox = &box
			}
			ev = append(ev, item)
		}
		out.Evidence = &ev
	}
	return out
}
