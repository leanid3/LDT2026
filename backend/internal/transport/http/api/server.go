// Package api — композиционный корень HTTP-слоя: единственное место, которому разрешено знать про
// все домены сразу (auth, objects, files, process, findings, protocol) и реализовывать
// gen.ServerInterface целиком. Сами доменные пакеты друг про друга ничего такого не знают — только
// через узкие структурно удовлетворяемые интерфейсы (docs/architecture.md, инверсия зависимостей);
// здесь эти "провода" наконец соединяются.
//
// Именованные (не анонимно встроенные) поля — у auth.Handler/objects.Handler/files.Handler/
// findings.Handler/protocol.Handler одинаковое имя типа "Handler", анонимное встраивание нескольких
// таких типов сразу дало бы конфликт имён полей; явные делегирующие методы ниже чуть многословнее,
// зато однозначны.
package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/auth"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/files"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/findings"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/matrix"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/objects"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/httpx"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/process"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/protocol"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/transport/http/gen"
)

type Server struct {
	auth        *auth.Handler
	objects     *objects.Handler
	files       *files.Handler
	findings    *findings.Handler
	protocol    *protocol.Handler
	filesRepo   *files.Repository
	protocolSvc *protocol.Service
	process     *process.Service
	objectsRepo *objects.Repository
	paramsRepo  *matrix.Repository
}

func NewServer(
	authHandler *auth.Handler, objectsHandler *objects.Handler, filesHandler *files.Handler,
	findingsHandler *findings.Handler, protocolHandler *protocol.Handler,
	filesRepo *files.Repository, protocolSvc *protocol.Service, processSvc *process.Service,
	objectsRepo *objects.Repository, paramsRepo *matrix.Repository,
) *Server {
	return &Server{
		auth: authHandler, objects: objectsHandler, files: filesHandler,
		findings: findingsHandler, protocol: protocolHandler,
		filesRepo: filesRepo, protocolSvc: protocolSvc, process: processSvc,
		objectsRepo: objectsRepo, paramsRepo: paramsRepo,
	}
}

var _ gen.ServerInterface = (*Server)(nil)

// --- auth ---
func (s *Server) Login(c *gin.Context)          { s.auth.Login(c) }
func (s *Server) GetCurrentUser(c *gin.Context) { s.auth.GetCurrentUser(c) }

// --- objects ---
func (s *Server) CreateObject(c *gin.Context)              { s.objects.CreateObject(c) }
func (s *Server) ListObjects(c *gin.Context)               { s.objects.ListObjects(c) }
func (s *Server) GetObject(c *gin.Context, id gen.IdParam) { s.objects.GetObject(c, id) }

// --- documents (files) ---
func (s *Server) UploadDocuments(c *gin.Context) { s.files.UploadDocuments(c) }
func (s *Server) ConfirmDocuments(c *gin.Context, processId gen.ProcessIdParam) {
	s.files.ConfirmDocuments(c, processId)
}
func (s *Server) GetRegistryTemplate(c *gin.Context, params gen.GetRegistryTemplateParams) {
	s.files.GetRegistryTemplate(c, params)
}
func (s *Server) GetProcessRegistryTemplate(c *gin.Context, processId gen.ProcessIdParam, params gen.GetProcessRegistryTemplateParams) {
	s.files.GetProcessRegistryTemplate(c, processId, params)
}
func (s *Server) UploadRegistry(c *gin.Context, processId gen.ProcessIdParam) {
	s.files.UploadRegistry(c, processId)
}

func (s *Server) GetFileDownloadUrl(c *gin.Context, id gen.IdParam) {
	s.files.GetFileDownloadUrl(c, id)
}

// --- matrix / object processes: читают из двух доменов сразу, поэтому живут здесь ---

func (s *Server) ListParams(c *gin.Context) {
	if _, ok := auth.Require(c); !ok {
		return
	}
	version, params, err := s.paramsRepo.ListActive(c.Request.Context())
	if err != nil {
		httpx.Fail(c, httpx.ErrInternal)
		return
	}
	items := make([]gen.Param, 0, len(params))
	for _, p := range params {
		item := gen.Param{Code: p.Code, Name: p.Name}
		setStr := func(dst **string, v string) {
			if v != "" {
				*dst = &v
			}
		}
		setStr(&item.Section, p.Section)
		setStr(&item.Unit, p.Unit)
		setStr(&item.SourcePd, p.SourcePD)
		setStr(&item.SourceRd, p.SourceRD)
		setStr(&item.SourceId, p.SourceID)
		setStr(&item.TriggerLogic, p.TriggerLogic)
		if p.ReviewPriority != "" {
			rp := gen.ParamReviewPriority(p.ReviewPriority)
			item.ReviewPriority = &rp
		}
		if p.DataType != "" {
			dt := gen.ParamDataType(p.DataType)
			item.DataType = &dt
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gen.ParamList{MatrixVersion: version, Items: items})
}

func (s *Server) ListObjectProcesses(c *gin.Context, id gen.IdParam) {
	if _, ok := auth.Require(c); !ok {
		return
	}
	if _, err := s.objectsRepo.Get(c.Request.Context(), uuid.UUID(id)); err != nil {
		if errors.Is(err, objects.ErrNotFound) {
			httpx.Fail(c, httpx.NewError(httpx.CodeNotFound, http.StatusNotFound, "объект не найден"))
			return
		}
		httpx.Fail(c, httpx.ErrInternal)
		return
	}
	list, err := s.process.ListByObject(c.Request.Context(), uuid.UUID(id))
	if err != nil {
		httpx.Fail(c, httpx.ErrInternal)
		return
	}
	items := make([]gen.ProcessSummary, 0, len(list))
	for _, p := range list {
		item := gen.ProcessSummary{
			Id: p.ID, ObjectId: p.ObjectID, Status: gen.ProcessStatus(p.Status),
			CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, FinalizedAt: p.FinalizedAt,
		}
		if p.Scenario != nil {
			sc := gen.Scenario(*p.Scenario)
			item.Scenario = &sc
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gen.ProcessList{Items: items})
}

// --- findings ---
func (s *Server) GetFinding(c *gin.Context, id gen.IdParam)    { s.findings.GetFinding(c, id) }
func (s *Server) DecideFinding(c *gin.Context, id gen.IdParam) { s.findings.DecideFinding(c, id) }

// --- protocol ---
func (s *Server) GetProtocol(c *gin.Context, id gen.IdParam) { s.protocol.GetProtocol(c, id) }

// --- processes (собираются из process.Service + files.Repository — единственный резон, почему это
// не могло остаться внутри internal/process без цикла импорта, см. docs/architecture.md) ---

func (s *Server) GetProcess(c *gin.Context, id gen.IdParam) {
	if _, ok := auth.Require(c); !ok {
		return
	}
	p, err := s.process.Get(c.Request.Context(), uuid.UUID(id))
	if err != nil {
		writeProcessError(c, err)
		return
	}
	c.JSON(http.StatusOK, toProcessDetail(s, c, p))
}

func (s *Server) ListProcessFiles(c *gin.Context, id gen.IdParam) {
	if _, ok := auth.Require(c); !ok {
		return
	}
	list, err := s.filesRepo.ListByProcess(c.Request.Context(), uuid.UUID(id))
	if err != nil {
		httpx.Fail(c, httpx.ErrInternal)
		return
	}

	items := make([]gen.FileInfo, 0, len(list))
	for _, f := range list {
		item := gen.FileInfo{
			Id: f.ID, OriginalName: f.OriginalName, CheckStatus: gen.FileCheckStatus(f.CheckStatus),
			IsCurrent: f.IsCurrent, UploadedAt: f.UploadedAt,
			DocumentCode: f.DocumentCode, Revision: f.Revision, ApprovalStatus: f.ApprovalStatus,
			SelectionStatus: f.SelectionStatus, SelectionReason: f.SelectionReason, Discipline: f.Discipline,
		}
		if f.DocStage != nil {
			ds := gen.DocStage(*f.DocStage)
			item.DocStage = &ds
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gen.ProcessFilesResponse{Items: items})
}

func (s *Server) StartProcess(c *gin.Context, id gen.IdParam) {
	if _, ok := auth.Require(c); !ok {
		return
	}
	p, err := s.process.Start(c.Request.Context(), uuid.UUID(id))
	if err != nil {
		writeProcessError(c, err)
		return
	}
	c.JSON(http.StatusOK, toProcessDetail(s, c, p))
}

func (s *Server) FinalizeProcess(c *gin.Context, id gen.IdParam) {
	claims, ok := auth.Require(c)
	if !ok {
		return
	}
	actorID, err := claims.UserID()
	if err != nil {
		httpx.Fail(c, httpx.NewError(httpx.CodeUnauthorized, http.StatusUnauthorized, "невалидный токен"))
		return
	}
	p, err := s.process.Finalize(c.Request.Context(), uuid.UUID(id), actorID)
	if err != nil {
		writeProcessError(c, err)
		return
	}
	c.JSON(http.StatusOK, toProcessDetail(s, c, p))
}

func (s *Server) UnfinalizeProcess(c *gin.Context, id gen.IdParam) {
	claims, ok := auth.RequireRole(c, auth.RoleSupervisor, auth.RoleAdmin)
	if !ok {
		return
	}
	actorID, err := claims.UserID()
	if err != nil {
		httpx.Fail(c, httpx.NewError(httpx.CodeUnauthorized, http.StatusUnauthorized, "невалидный токен"))
		return
	}

	var req gen.UnfinalizeRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Reason == "" {
		httpx.Fail(c, httpx.NewError(httpx.CodeValidationFailed, http.StatusBadRequest, "поле reason обязательно"))
		return
	}

	p, err := s.process.Unfinalize(c.Request.Context(), uuid.UUID(id), actorID, req.Reason)
	if err != nil {
		writeProcessError(c, err)
		return
	}
	c.JSON(http.StatusOK, toProcessDetail(s, c, p))
}

func (s *Server) GetProcessSync(c *gin.Context, id gen.IdParam) {
	if _, ok := auth.Require(c); !ok {
		return
	}

	// Синхронизация начинается только после финализации (outbox rin.sync.requested пишется в
	// process.Service.Finalize) — до этого момента протокола со sync_status ещё может не быть.
	p, err := s.protocolSvc.Get(c.Request.Context(), uuid.UUID(id))
	if err != nil {
		if errors.Is(err, protocol.ErrNotFound) {
			c.JSON(http.StatusOK, gen.SyncStatusResponse{SyncStatus: gen.SyncStatus(protocol.SyncStatusNotRequired)})
			return
		}
		httpx.Fail(c, httpx.ErrInternal)
		return
	}
	c.JSON(http.StatusOK, gen.SyncStatusResponse{SyncStatus: gen.SyncStatus(p.Protocol.SyncStatus)})
}

func toProcessDetail(s *Server, c *gin.Context, p process.Process) gen.ProcessDetail {
	statuses, err := s.process.UploadStatuses(c.Request.Context(), p.ID)
	if err != nil {
		statuses = nil
	}
	entries := make([]gen.UploadStatusEntry, 0, len(statuses))
	for _, e := range statuses {
		entries = append(entries, gen.UploadStatusEntry{Stage: gen.DocStage(e.Stage), Status: gen.UploadStageStatus(e.Status)})
	}

	detail := gen.ProcessDetail{
		Id: p.ID, ObjectId: p.ObjectID, Status: gen.ProcessStatus(p.Status),
		MatrixVersion: p.MatrixVersion, UploadStatus: entries,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, FinalizedAt: p.FinalizedAt,
	}
	if p.Scenario != nil {
		sc := gen.Scenario(*p.Scenario)
		detail.Scenario = &sc
	}
	return detail
}

func writeProcessError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, process.ErrNotFound):
		httpx.Fail(c, httpx.NewError(httpx.CodeNotFound, http.StatusNotFound, "процесс не найден"))
	case errors.Is(err, process.ErrInvalidTransition), errors.Is(err, process.ErrConflict),
		errors.Is(err, process.ErrHasPendingCandidates), errors.Is(err, process.ErrNoAcceptedFiles),
		errors.Is(err, process.ErrReasonRequired):
		httpx.Fail(c, httpx.NewError(httpx.CodeConflict, http.StatusConflict, err.Error()))
	default:
		httpx.Fail(c, httpx.ErrInternal)
	}
}
