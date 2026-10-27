package httpapi

import (
	"net/http"
	"regexp"

	"certadmin/internal/service"

	"github.com/gin-gonic/gin"
)

// Server wires the HTTP contract to the service layer.
type Server struct {
	svc           *service.Service
	engine        *gin.Engine
	exportSem     chan struct{}
	exportRowHook func(service.CertificateCSVRow, int) error
}

func New(svc *service.Service) *Server {
	s := &Server{
		svc:       svc,
		exportSem: make(chan struct{}, 2),
	}
	engine := gin.New()
	engine.HandleMethodNotAllowed = true
	engine.RedirectTrailingSlash = false
	engine.RedirectFixedPath = false
	engine.RemoveExtraSlash = false
	engine.Use(requestIDMiddleware(), recoveryMiddleware())
	engine.NoRoute(s.handleNotFound)
	engine.NoMethod(s.handleMethodNotAllowed)

	api := engine.Group("/api/v1", s.authMiddleware())
	api.GET("/me", s.handleMe)
	api.GET("/courses", s.handleCourses)
	api.GET("/courses/:courseId/enrollments", s.handleCourseEnrollments)
	api.POST("/courses/:courseId/bulk-operations", s.handleBulkIssue)
	api.GET("/learners/:learnerId/enrollments", s.handleLearnerEnrollments)
	api.GET("/certificates", s.handleCertificates)
	api.GET("/certificates.csv", s.handleCertificateCSV)
	api.GET("/certificates/:certificateId", s.handleCertificateDetail)
	api.PUT("/certificates/:certificateId/revocation", s.handleRevocation)

	s.engine = engine
	return s
}

func (s *Server) Handler() http.Handler { return s.engine }

func (s *Server) handleNotFound(c *gin.Context) {
	s.writeError(c, &apiError{Status: http.StatusNotFound, Code: "NOT_FOUND", Message: "未知 API 路径"})
}

type routeRule struct {
	pattern *regexp.Regexp
	methods []string
}

var routeRules = []routeRule{
	{regexp.MustCompile(`^/api/v1/me$`), []string{"GET"}},
	{regexp.MustCompile(`^/api/v1/courses$`), []string{"GET"}},
	{regexp.MustCompile(`^/api/v1/courses/[1-9][0-9]*/enrollments$`), []string{"GET"}},
	{regexp.MustCompile(`^/api/v1/courses/[1-9][0-9]*/bulk-operations$`), []string{"POST"}},
	{regexp.MustCompile(`^/api/v1/learners/[1-9][0-9]*/enrollments$`), []string{"GET"}},
	{regexp.MustCompile(`^/api/v1/certificates$`), []string{"GET"}},
	{regexp.MustCompile(`^/api/v1/certificates\.csv$`), []string{"GET"}},
	{regexp.MustCompile(`^/api/v1/certificates/[1-9][0-9]*$`), []string{"GET"}},
	{regexp.MustCompile(`^/api/v1/certificates/[1-9][0-9]*/revocation$`), []string{"PUT"}},
}

func (s *Server) handleMethodNotAllowed(c *gin.Context) {
	for _, rule := range routeRules {
		if rule.pattern.MatchString(c.Request.URL.Path) {
			c.Header("Allow", rule.methods[0])
			s.writeError(c, &apiError{
				Status:  http.StatusMethodNotAllowed,
				Code:    "METHOD_NOT_ALLOWED",
				Message: "该路径不支持此 HTTP 方法",
			})
			return
		}
	}
	s.handleNotFound(c)
}
