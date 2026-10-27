package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *Service) Me(actor *Actor) MeResponse {
	return MeResponse{
		Admin: AdminRef{ID: actor.AdminID, Email: actor.Email},
		Tenant: TenantRef{
			ID:       actor.TenantID,
			Code:     actor.TenantCode,
			Name:     actor.TenantName,
			Timezone: actor.Timezone,
		},
	}
}

func (s *Service) ListCourses(ctx context.Context, tenantID int64, page, pageSize int) (Page[CourseRef], error) {
	out := Page[CourseRef]{Items: []CourseRef{}, Page: page, PageSize: pageSize}
	tx, err := s.db.Read.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, mapDBError(err)
	}
	defer tx.Rollback()

	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM courses WHERE tenant_id = ?`, tenantID).Scan(&out.Total); err != nil {
		return out, mapDBError(err)
	}
	offset := int64(page-1) * int64(pageSize)
	rows, err := tx.QueryContext(ctx, `
		SELECT id, title FROM courses
		WHERE tenant_id = ?
		ORDER BY id ASC
		LIMIT ? OFFSET ?`, tenantID, pageSize, offset)
	if err != nil {
		return out, mapDBError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var item CourseRef
		if err := rows.Scan(&item.ID, &item.Title); err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	return out, mapDBError(rows.Err())
}

// EnrollmentQuery carries the optional filters shared by both enrollment lists.
type EnrollmentQuery struct {
	CompletionStatus string
	CourseID         int64
	Page             int
	PageSize         int
}

func (s *Service) CourseEnrollments(ctx context.Context, tenantID, courseID int64, q EnrollmentQuery) (Page[EnrollmentItem], error) {
	out := Page[EnrollmentItem]{Items: []EnrollmentItem{}, Page: q.Page, PageSize: q.PageSize}
	tx, err := s.db.Read.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, mapDBError(err)
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM courses WHERE tenant_id = ? AND id = ?`, tenantID, courseID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, ErrNotFound
		}
		return out, mapDBError(err)
	}

	where := []string{"e.tenant_id = ?", "e.course_id = ?"}
	args := []any{tenantID, courseID}
	if q.CompletionStatus != "" {
		where = append(where, "e.completion_status = ?")
		args = append(args, q.CompletionStatus)
	}
	return s.queryEnrollments(ctx, tx, strings.Join(where, " AND "), args, out)
}

func (s *Service) LearnerEnrollments(ctx context.Context, tenantID, learnerID int64, q EnrollmentQuery) (Page[EnrollmentItem], error) {
	out := Page[EnrollmentItem]{Items: []EnrollmentItem{}, Page: q.Page, PageSize: q.PageSize}
	tx, err := s.db.Read.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, mapDBError(err)
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM learners WHERE tenant_id = ? AND id = ?`, tenantID, learnerID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, ErrNotFound
		}
		return out, mapDBError(err)
	}

	where := []string{"e.tenant_id = ?", "e.learner_id = ?"}
	args := []any{tenantID, learnerID}
	if q.CourseID > 0 {
		where = append(where, "e.course_id = ?")
		args = append(args, q.CourseID)
	}
	if q.CompletionStatus != "" {
		where = append(where, "e.completion_status = ?")
		args = append(args, q.CompletionStatus)
	}
	return s.queryEnrollments(ctx, tx, strings.Join(where, " AND "), args, out)
}

func (s *Service) queryEnrollments(ctx context.Context, tx *sql.Tx, where string, args []any, out Page[EnrollmentItem]) (Page[EnrollmentItem], error) {
	countSQL := `SELECT COUNT(*) FROM enrollments e WHERE ` + where
	if err := tx.QueryRowContext(ctx, countSQL, args...).Scan(&out.Total); err != nil {
		return out, mapDBError(err)
	}

	offset := int64(out.Page-1) * int64(out.PageSize)
	listArgs := append(append([]any{}, args...), out.PageSize, offset)
	rows, err := tx.QueryContext(ctx, `
		SELECT e.id, e.completion_status, e.completed_at,
		       l.id, l.name,
		       co.id, co.title,
		       c.id, c.serial_number, c.status, c.issued_at, c.issued_local_date
		FROM enrollments e
		JOIN learners l ON l.tenant_id = e.tenant_id AND l.id = e.learner_id
		JOIN courses co ON co.tenant_id = e.tenant_id AND co.id = e.course_id
		LEFT JOIN certificates c ON c.tenant_id = e.tenant_id AND c.enrollment_id = e.id
		WHERE `+where+`
		ORDER BY e.id ASC
		LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return out, mapDBError(err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			item        EnrollmentItem
			completedAt sql.NullString
			certID      sql.NullInt64
			certSerial  sql.NullString
			certStatus  sql.NullString
			certIssued  sql.NullString
			certDate    sql.NullString
		)
		if err := rows.Scan(&item.ID, &item.CompletionStatus, &completedAt,
			&item.Learner.ID, &item.Learner.Name,
			&item.Course.ID, &item.Course.Title,
			&certID, &certSerial, &certStatus, &certIssued, &certDate); err != nil {
			return out, err
		}
		if completedAt.Valid {
			v := completedAt.String
			item.CompletedAt = &v
		}
		if certID.Valid {
			item.Certificate = &CertificateSummary{
				ID:              certID.Int64,
				SerialNumber:    certSerial.String,
				Status:          certStatus.String,
				IssuedAt:        certIssued.String,
				IssuedLocalDate: certDate.String,
			}
		}
		item.IssuanceEligibility = eligibility(item.CompletionStatus, item.Certificate)
		out.Items = append(out.Items, item)
	}
	return out, mapDBError(rows.Err())
}

func eligibility(completionStatus string, cert *CertificateSummary) Eligibility {
	if completionStatus != "COMPLETED" {
		reason := "COURSE_INCOMPLETE"
		return Eligibility{Eligible: false, ReasonCode: &reason}
	}
	if cert != nil {
		reason := "ALREADY_CERTIFIED"
		if cert.Status == "REVOKED" {
			reason = "CERTIFICATE_REVOKED"
		}
		return Eligibility{Eligible: false, ReasonCode: &reason}
	}
	return Eligibility{Eligible: true}
}

func (s *Service) ListCertificates(ctx context.Context, tenantID int64, f CertificateFilter) (Page[CertificateListItem], error) {
	out := Page[CertificateListItem]{Items: []CertificateListItem{}, Page: f.Page, PageSize: f.PageSize}
	where, args := certificateWhere(tenantID, f)

	tx, err := s.db.Read.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, mapDBError(err)
	}
	defer tx.Rollback()

	countSQL := `SELECT COUNT(*) FROM certificates c
		JOIN learners l ON l.tenant_id = c.tenant_id AND l.id = c.learner_id
		WHERE ` + where
	if err := tx.QueryRowContext(ctx, countSQL, args...).Scan(&out.Total); err != nil {
		return out, mapDBError(err)
	}

	listArgs := append(append([]any{}, args...), f.PageSize, int64(f.Page-1)*int64(f.PageSize))
	rows, err := tx.QueryContext(ctx, `
		SELECT c.id, c.serial_number, c.status, c.issued_at, c.issued_local_date,
		       l.id, l.name, co.id, co.title
		FROM certificates c
		JOIN learners l ON l.tenant_id = c.tenant_id AND l.id = c.learner_id
		JOIN courses co ON co.tenant_id = c.tenant_id AND co.id = c.course_id
		WHERE `+where+`
		ORDER BY `+certificateOrder(f)+`
		LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return out, mapDBError(err)
	}
	defer rows.Close()

	for rows.Next() {
		var item CertificateListItem
		if err := rows.Scan(&item.ID, &item.SerialNumber, &item.Status, &item.IssuedAt, &item.IssuedLocalDate,
			&item.Learner.ID, &item.Learner.Name, &item.Course.ID, &item.Course.Title); err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	return out, mapDBError(rows.Err())
}

func certificateWhere(tenantID int64, f CertificateFilter) (string, []any) {
	where := []string{"c.tenant_id = ?"}
	args := []any{tenantID}
	if f.Q != "" {
		pattern := "%" + escapeLike(f.Q) + "%"
		where = append(where, `(l.name LIKE ? ESCAPE '\' OR c.serial_number LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern)
	}
	if f.Status != "" {
		where = append(where, "c.status = ?")
		args = append(args, f.Status)
	}
	if f.CourseID > 0 {
		where = append(where, "c.course_id = ?")
		args = append(args, f.CourseID)
	}
	if f.LearnerID > 0 {
		where = append(where, "c.learner_id = ?")
		args = append(args, f.LearnerID)
	}
	return strings.Join(where, " AND "), args
}

func certificateOrder(f CertificateFilter) string {
	direction := "ASC"
	if strings.EqualFold(f.SortOrder, "desc") {
		direction = "DESC"
	}
	var key string
	switch f.SortBy {
	case "learnerName":
		key = "l.name"
	case "serialNumber":
		key = "c.serial_number"
	default:
		key = "c.issued_local_date"
	}
	// certificate.id ASC is always the deterministic tie-breaker, even when the
	// primary order is descending (A6 export stability).
	return key + " " + direction + ", c.id ASC"
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

func (s *Service) CertificateDetail(ctx context.Context, actor *Actor, certificateID int64) (CertificateDetail, error) {
	var (
		out          CertificateDetail
		revokedAt    sql.NullString
		revokedID    sql.NullInt64
		revokedEmail sql.NullString
	)
	err := s.db.Read.QueryRowContext(ctx, `
		SELECT c.id, c.serial_number, c.status, c.issued_at, c.issued_local_date,
		       l.id, l.name, co.id, co.title,
		       ia.id, ia.email,
		       c.revoked_at, ra.id, ra.email
		FROM certificates c
		JOIN learners l ON l.tenant_id = c.tenant_id AND l.id = c.learner_id
		JOIN courses co ON co.tenant_id = c.tenant_id AND co.id = c.course_id
		JOIN admins ia ON ia.tenant_id = c.tenant_id AND ia.id = c.issued_by_admin_id
		LEFT JOIN admins ra ON ra.tenant_id = c.tenant_id AND ra.id = c.revoked_by_admin_id
		WHERE c.tenant_id = ? AND c.id = ?`, actor.TenantID, certificateID).
		Scan(&out.ID, &out.SerialNumber, &out.Status, &out.IssuedAt, &out.IssuedLocalDate,
			&out.Learner.ID, &out.Learner.Name, &out.Course.ID, &out.Course.Title,
			&out.IssuedBy.ID, &out.IssuedBy.Email,
			&revokedAt, &revokedID, &revokedEmail)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, ErrNotFound
		}
		return out, mapDBError(err)
	}
	issued, err := parseUTC(out.IssuedAt)
	if err != nil {
		return out, fmt.Errorf("parse issued_at: %w", err)
	}
	out.IssuedLocalAt = localDateTime(issued, actor.Location)
	out.Timezone = actor.Timezone
	if revokedAt.Valid {
		value := revokedAt.String
		out.RevokedAt = &value
		out.RevokedBy = &AdminRef{ID: revokedID.Int64, Email: revokedEmail.String}
	}
	return out, nil
}
