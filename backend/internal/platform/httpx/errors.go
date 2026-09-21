package httpx

import "net/http"

// ErrorCode — коды ошибок API. Раздел ТЗ конкретизируется по мере добавления доменов (M1+).
type ErrorCode string

const (
	CodeValidationFailed ErrorCode = "VALIDATION_FAILED"
	CodeNotFound         ErrorCode = "NOT_FOUND"
	CodeUnauthorized     ErrorCode = "UNAUTHORIZED"
	CodeForbidden        ErrorCode = "FORBIDDEN"
	CodeConflict         ErrorCode = "CONFLICT"
	CodeInternal         ErrorCode = "INTERNAL_ERROR"
)

// AppError — доменная ошибка с HTTP-статусом и деталями.
type AppError struct {
	Code       ErrorCode
	Message    string
	HTTPStatus int
	Details    map[string]any
	Internal   error
}

func (e *AppError) Error() string {
	if e.Internal != nil {
		return e.Message + ": " + e.Internal.Error()
	}
	return e.Message
}

func (e *AppError) Unwrap() error { return e.Internal }

func NewError(code ErrorCode, status int, message string) *AppError {
	return &AppError{Code: code, Message: message, HTTPStatus: status}
}

func (e *AppError) WithDetails(details map[string]any) *AppError {
	e.Details = details
	return e
}

var ErrInternal = NewError(CodeInternal, http.StatusInternalServerError, "внутренняя ошибка сервера")

// errorBody — формат ответа согласно backend-plan.md §7:
// { "error": { "code", "message", "details" }, "request_id" }.
type errorBody struct {
	Error     errorDetail `json:"error"`
	RequestID string      `json:"request_id"`
}

type errorDetail struct {
	Code    ErrorCode      `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func ErrorResponse(err *AppError, requestID string) (int, any) {
	return err.HTTPStatus, errorBody{
		Error: errorDetail{
			Code:    err.Code,
			Message: err.Message,
			Details: err.Details,
		},
		RequestID: requestID,
	}
}
