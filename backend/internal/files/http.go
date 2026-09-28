package files

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/auth"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/httpx"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/process"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/transport/http/gen"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) UploadDocuments(c *gin.Context) {
	claims, ok := auth.Require(c)
	if !ok {
		return
	}
	actorID, err := claims.UserID()
	if err != nil {
		httpx.Fail(c, httpx.NewError(httpx.CodeUnauthorized, http.StatusUnauthorized, "невалидный токен"))
		return
	}

	var req gen.DocumentsUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Files) == 0 {
		httpx.Fail(c, httpx.NewError(httpx.CodeValidationFailed, http.StatusBadRequest, "тело запроса невалидно: нужны object_id и непустой files[]"))
		return
	}

	reqFiles := make([]UploadFileRequest, 0, len(req.Files))
	for _, f := range req.Files {
		reqFiles = append(reqFiles, UploadFileRequest{OriginalName: f.OriginalName, SizeBytes: f.SizeBytes})
	}

	var processID *uuid.UUID
	if req.ProcessId != nil {
		v := uuid.UUID(*req.ProcessId)
		processID = &v
	}

	pid, uploads, err := h.service.Upload(c.Request.Context(), uuid.UUID(req.ObjectId), processID, actorID, reqFiles)
	if err != nil {
		writeServiceError(c, err)
		return
	}

	resp := gen.DocumentsUploadResponse{ProcessId: pid, Files: make([]gen.PresignedUpload, 0, len(uploads))}
	for _, u := range uploads {
		resp.Files = append(resp.Files, gen.PresignedUpload{
			FileId: u.FileID, OriginalName: u.OriginalName, UploadUrl: u.UploadURL,
			UploadFields: u.UploadFields, ExpiresAt: u.ExpiresAt,
		})
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) ConfirmDocuments(c *gin.Context, processID gen.ProcessIdParam) {
	if _, ok := auth.Require(c); !ok {
		return
	}

	results, err := h.service.Confirm(c.Request.Context(), uuid.UUID(processID))
	if err != nil {
		httpx.Fail(c, httpx.ErrInternal)
		return
	}

	resp := gen.ConfirmResponse{Files: make([]gen.ConfirmedFile, 0, len(results))}
	for _, r := range results {
		item := gen.ConfirmedFile{FileId: r.FileID, CheckStatus: gen.FileCheckStatus(r.CheckStatus)}
		if r.CheckError != "" {
			item.CheckError = &r.CheckError
		}
		if r.PageCount > 0 {
			pc := r.PageCount
			item.PageCount = &pc
		}
		resp.Files = append(resp.Files, item)
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) UploadRegistry(c *gin.Context, processID gen.ProcessIdParam) {
	if _, ok := auth.Require(c); !ok {
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		httpx.Fail(c, httpx.NewError(httpx.CodeValidationFailed, http.StatusBadRequest, "ожидается multipart-поле file"))
		return
	}
	src, err := fileHeader.Open()
	if err != nil {
		httpx.Fail(c, httpx.ErrInternal)
		return
	}
	defer func() { _ = src.Close() }()

	result, err := h.service.UploadRegistry(c.Request.Context(), uuid.UUID(processID), fileHeader.Filename, src)
	if err != nil {
		writeServiceError(c, err)
		return
	}

	resp := gen.RegistryUploadResponse{TotalRows: result.TotalRows, Matched: result.Matched, Unmatched: result.Unmatched, Invalid: &result.Invalid}
	if len(result.Errors) > 0 {
		resp.Errors = &result.Errors
	}
	if len(result.Warnings) > 0 {
		resp.Warnings = &result.Warnings
	}
	c.JSON(http.StatusOK, resp)
}

func writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrValidation):
		httpx.Fail(c, httpx.NewError(httpx.CodeValidationFailed, http.StatusBadRequest, err.Error()))
	case errors.Is(err, process.ErrConflict):
		httpx.Fail(c, httpx.NewError(httpx.CodeConflict, http.StatusConflict, err.Error()))
	case errors.Is(err, process.ErrNotFound):
		httpx.Fail(c, httpx.NewError(httpx.CodeNotFound, http.StatusNotFound, "процесс не найден"))
	default:
		httpx.Fail(c, httpx.ErrInternal)
	}
}

// GetFileDownloadUrl — GET /files/{id}/download-url.
func (h *Handler) GetFileDownloadUrl(c *gin.Context, id gen.IdParam) {
	if _, ok := auth.Require(c); !ok {
		return
	}
	u, expires, err := h.service.DownloadURL(c.Request.Context(), uuid.UUID(id))
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.Fail(c, httpx.NewError(httpx.CodeNotFound, http.StatusNotFound, "файл не найден"))
	case errors.Is(err, ErrNotDownloadable):
		httpx.Fail(c, httpx.NewError(httpx.CodeConflict, http.StatusConflict, "файл не прошёл проверку и недоступен для скачивания"))
	case err != nil:
		httpx.Fail(c, httpx.ErrInternal)
	default:
		c.JSON(http.StatusOK, gen.FileDownloadUrl{Url: u, ExpiresAt: expires})
	}
}

// GetRegistryTemplate — GET /documents/registry-template: пустой шаблон.
func (h *Handler) GetRegistryTemplate(c *gin.Context, params gen.GetRegistryTemplateParams) {
	if _, ok := auth.Require(c); !ok {
		return
	}
	format := ""
	if params.Format != nil {
		format = string(*params.Format)
	}
	h.writeTemplate(c, nil, format)
}

// GetProcessRegistryTemplate — GET /documents/{process_id}/registry-template: предзаполненный файлами процесса.
func (h *Handler) GetProcessRegistryTemplate(c *gin.Context, processID gen.ProcessIdParam, params gen.GetProcessRegistryTemplateParams) {
	if _, ok := auth.Require(c); !ok {
		return
	}
	format := ""
	if params.Format != nil {
		format = string(*params.Format)
	}
	id := uuid.UUID(processID)
	h.writeTemplate(c, &id, format)
}

func (h *Handler) writeTemplate(c *gin.Context, processID *uuid.UUID, format string) {
	tpl, err := h.service.RegistryTemplate(c.Request.Context(), processID, format)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+tpl.Filename+`"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, tpl.ContentType, tpl.Data)
}
