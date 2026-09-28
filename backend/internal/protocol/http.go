package protocol

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/auth"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/findings"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/httpx"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/transport/http/gen"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetProtocol(c *gin.Context, processID gen.IdParam) {
	if _, ok := auth.Require(c); !ok {
		return
	}

	a, err := h.service.Get(c.Request.Context(), uuid.UUID(processID))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Fail(c, httpx.NewError(httpx.CodeNotFound, http.StatusNotFound, "протокол ещё не сформирован (процесс не в READY и позже)"))
			return
		}
		httpx.Fail(c, httpx.ErrInternal)
		return
	}

	resp := gen.ProtocolResponse{
		ProcessId: a.Protocol.ProcessID, Version: a.Protocol.Version,
		Status: gen.ProtocolResponseStatus(a.Protocol.Status), MatrixVersion: a.Protocol.MatrixVersion,
		DatasetVersion: a.Protocol.DatasetVersion, ModelVersion: a.Protocol.ModelVersion,
		CreatedAt: &a.Protocol.CreatedAt, FinalizedAt: a.Protocol.FinalizedAt,
		Findings: make([]gen.Finding, 0, len(a.Findings)),
	}
	for _, f := range a.Findings {
		resp.Findings = append(resp.Findings, findings.ToAPI(f))
	}

	c.JSON(http.StatusOK, resp)
}
