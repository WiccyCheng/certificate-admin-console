package httpapi

import (
	"errors"
	"log"
	"net/http"

	"certadmin/internal/service"

	"github.com/gin-gonic/gin"
)

type errorDetail struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

type apiError struct {
	Status  int
	Code    string
	Message string
	Details []errorDetail
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

func badRequest(message, field, reason string) *apiError {
	e := &apiError{Status: http.StatusBadRequest, Code: "INVALID_ARGUMENT", Message: message}
	if field != "" {
		e.Details = []errorDetail{{Field: field, Reason: reason}}
	}
	return e
}

func unsupportedMedia() *apiError {
	return &apiError{
		Status:  http.StatusUnsupportedMediaType,
		Code:    "UNSUPPORTED_MEDIA_TYPE",
		Message: "非空请求体必须使用 application/json",
	}
}

func unauthorized(message string) *apiError {
	return &apiError{Status: http.StatusUnauthorized, Code: "UNAUTHENTICATED", Message: message}
}

type errorResponse struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code      string        `json:"code"`
	Message   string        `json:"message"`
	RequestID string        `json:"requestId"`
	Details   []errorDetail `json:"details,omitempty"`
}

func (s *Server) writeError(c *gin.Context, e *apiError) {
	if c.Writer.Written() {
		return
	}
	if e.Status == http.StatusUnauthorized {
		c.Header("WWW-Authenticate", `Bearer realm="certificate-admin"`)
	}
	if e.Status == http.StatusServiceUnavailable {
		c.Header("Retry-After", "1")
	}
	c.AbortWithStatusJSON(e.Status, errorResponse{Error: errorPayload{
		Code:      e.Code,
		Message:   e.Message,
		RequestID: requestID(c),
		Details:   e.Details,
	}})
}

func (s *Server) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		s.writeError(c, &apiError{Status: http.StatusNotFound, Code: "NOT_FOUND", Message: "资源不存在"})
	case errors.Is(err, service.ErrIdempotencyConflict):
		s.writeError(c, &apiError{
			Status:  http.StatusConflict,
			Code:    "IDEMPOTENCY_CONFLICT",
			Message: "该幂等键已用于不同的请求",
			Details: []errorDetail{{Field: "Idempotency-Key", Reason: "REQUEST_MISMATCH"}},
		})
	case errors.Is(err, service.ErrBusy):
		s.writeError(c, &apiError{Status: http.StatusServiceUnavailable, Code: "TEMPORARILY_UNAVAILABLE", Message: "数据库暂时繁忙，请重试"})
	case errors.Is(err, service.ErrUnauthenticated):
		s.writeError(c, unauthorized("身份凭证无效或缺失"))
	default:
		log.Printf("request %s failed: %v", requestID(c), err)
		s.writeError(c, &apiError{Status: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "服务器内部错误"})
	}
}
