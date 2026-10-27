package httpapi

import (
	"encoding/json"
	"net/http"

	"certadmin/internal/service"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleMe(c *gin.Context) {
	if _, aerr := parseQuery(c); aerr != nil {
		s.writeError(c, aerr)
		return
	}
	c.JSON(http.StatusOK, s.svc.Me(actorFrom(c)))
}

func (s *Server) handleCourses(c *gin.Context) {
	values, aerr := parseQuery(c, "page", "pageSize")
	if aerr != nil {
		s.writeError(c, aerr)
		return
	}
	page, pageSize, aerr := pageParams(values)
	if aerr != nil {
		s.writeError(c, aerr)
		return
	}
	result, err := s.svc.ListCourses(c.Request.Context(), actorFrom(c).TenantID, page, pageSize)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) handleCourseEnrollments(c *gin.Context) {
	actor := actorFrom(c)
	courseID, aerr := parsePathID(c, "courseId")
	if aerr != nil {
		s.writeError(c, aerr)
		return
	}
	values, aerr := parseQuery(c, "completionStatus", "page", "pageSize")
	if aerr != nil {
		s.writeError(c, aerr)
		return
	}
	completion, aerr := parseCompletion(values)
	if aerr != nil {
		s.writeError(c, aerr)
		return
	}
	page, pageSize, aerr := pageParams(values)
	if aerr != nil {
		s.writeError(c, aerr)
		return
	}
	result, err := s.svc.CourseEnrollments(c.Request.Context(), actor.TenantID, courseID, service.EnrollmentQuery{
		CompletionStatus: completion,
		Page:             page,
		PageSize:         pageSize,
	})
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) handleLearnerEnrollments(c *gin.Context) {
	actor := actorFrom(c)
	learnerID, aerr := parsePathID(c, "learnerId")
	if aerr != nil {
		s.writeError(c, aerr)
		return
	}
	values, aerr := parseQuery(c, "courseId", "completionStatus", "page", "pageSize")
	if aerr != nil {
		s.writeError(c, aerr)
		return
	}
	courseID, _, aerr := positiveIntParam(values, "courseId")
	if aerr != nil {
		s.writeError(c, aerr)
		return
	}
	completion, aerr := parseCompletion(values)
	if aerr != nil {
		s.writeError(c, aerr)
		return
	}
	page, pageSize, aerr := pageParams(values)
	if aerr != nil {
		s.writeError(c, aerr)
		return
	}
	result, err := s.svc.LearnerEnrollments(c.Request.Context(), actor.TenantID, learnerID, service.EnrollmentQuery{
		CourseID:         courseID,
		CompletionStatus: completion,
		Page:             page,
		PageSize:         pageSize,
	})
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) handleCertificates(c *gin.Context) {
	actor := actorFrom(c)
	filter, aerr := parseCertificateFilter(c, true)
	if aerr != nil {
		s.writeError(c, aerr)
		return
	}
	result, err := s.svc.ListCertificates(c.Request.Context(), actor.TenantID, filter)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) handleCertificateDetail(c *gin.Context) {
	actor := actorFrom(c)
	if _, aerr := parseQuery(c); aerr != nil {
		s.writeError(c, aerr)
		return
	}
	certificateID, aerr := parsePathID(c, "certificateId")
	if aerr != nil {
		s.writeError(c, aerr)
		return
	}
	result, err := s.svc.CertificateDetail(c.Request.Context(), actor, certificateID)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) handleBulkIssue(c *gin.Context) {
	actor := actorFrom(c)
	courseID, aerr := parsePathID(c, "courseId")
	if aerr != nil {
		s.writeError(c, aerr)
		return
	}
	if _, aerr := parseQuery(c); aerr != nil {
		s.writeError(c, aerr)
		return
	}
	if aerr := validateEmptyJSONBody(c); aerr != nil {
		s.writeError(c, aerr)
		return
	}
	key, aerr := idempotencyKey(c)
	if aerr != nil {
		s.writeError(c, aerr)
		return
	}
	body, created, err := s.svc.BulkIssue(c.Request.Context(), actor, courseID, key)
	if err != nil {
		s.fail(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.Data(status, "application/json; charset=utf-8", body)
}

func (s *Server) handleRevocation(c *gin.Context) {
	actor := actorFrom(c)
	certificateID, aerr := parsePathID(c, "certificateId")
	if aerr != nil {
		s.writeError(c, aerr)
		return
	}
	if _, aerr := parseQuery(c); aerr != nil {
		s.writeError(c, aerr)
		return
	}
	if aerr := validateEmptyJSONBody(c); aerr != nil {
		s.writeError(c, aerr)
		return
	}
	result, created, err := s.svc.Revoke(c.Request.Context(), actor, certificateID)
	if err != nil {
		s.fail(c, err)
		return
	}
	body, err := json.Marshal(result)
	if err != nil {
		s.fail(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.Data(status, "application/json; charset=utf-8", body)
}
