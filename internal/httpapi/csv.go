package httpapi

import (
	"bufio"
	"log"
	"net/http"
	"strings"
	"unicode"

	"certadmin/internal/service"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleCertificateCSV(c *gin.Context) {
	actor := actorFrom(c)
	filter, aerr := parseCertificateFilter(c, false)
	if aerr != nil {
		s.writeError(c, aerr)
		return
	}

	select {
	case s.exportSem <- struct{}{}:
		defer func() { <-s.exportSem }()
	default:
		s.writeError(c, &apiError{
			Status:  http.StatusServiceUnavailable,
			Code:    "TEMPORARILY_UNAVAILABLE",
			Message: "导出任务繁忙，请重试",
		})
		return
	}

	var (
		started bool
		writer  *bufio.Writer
		rows    int
	)
	err := s.svc.StreamCertificates(c.Request.Context(), actor.TenantID, filter,
		func() {
			c.Header("Content-Type", "text/csv; charset=utf-8")
			c.Header("Content-Disposition", `attachment; filename="certificates.csv"`)
			c.Status(http.StatusOK)
			c.Writer.WriteHeaderNow()
			started = true
			writer = bufio.NewWriterSize(c.Writer, 64*1024)
			_, _ = writer.WriteString("learnerName,courseTitle,serialNumber,issuedLocalDate\r\n")
		},
		func(row service.CertificateCSVRow) error {
			rows++
			if s.exportRowHook != nil {
				if err := s.exportRowHook(row, rows); err != nil {
					return err
				}
			}
			if err := writeCSVRecord(writer,
				protectFormula(row.LearnerName),
				protectFormula(row.CourseTitle),
				row.SerialNumber,
				row.IssuedLocalDate,
			); err != nil {
				return err
			}
			if rows%200 == 0 {
				return writer.Flush()
			}
			return nil
		})
	if err != nil {
		if !started {
			s.fail(c, err)
			return
		}
		log.Printf("request %s aborted CSV export after %d rows: %v", requestID(c), rows, err)
		panic(http.ErrAbortHandler)
	}
	if err := writer.Flush(); err != nil {
		log.Printf("request %s failed to flush CSV: %v", requestID(c), err)
		panic(http.ErrAbortHandler)
	}
}

func writeCSVRecord(w *bufio.Writer, fields ...string) error {
	for i, field := range fields {
		if i > 0 {
			if err := w.WriteByte(','); err != nil {
				return err
			}
		}
		if _, err := w.WriteString(csvField(field)); err != nil {
			return err
		}
	}
	_, err := w.WriteString("\r\n")
	return err
}

func csvField(value string) string {
	if !needsQuoting(value) {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func needsQuoting(value string) bool {
	if value == "" {
		return false
	}
	if strings.ContainsAny(value, ",\"\r\n") {
		return true
	}
	if value[0] == ' ' || value[0] == '\t' {
		return true
	}
	if value[len(value)-1] == ' ' || value[len(value)-1] == '\t' {
		return true
	}
	return false
}

// protectFormula prefixes user-controlled text that could be interpreted as a
// spreadsheet formula. serialNumber is system generated and is not sanitized.
func protectFormula(value string) string {
	trimmed := strings.TrimLeftFunc(value, unicode.IsSpace)
	if trimmed == "" {
		return value
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}
