package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// BulkIssue performs a synchronous idempotent issuance for every enrollment in
// the course. The returned bytes are the stored response, so replays are
// byte-identical to the first response.
func (s *Service) BulkIssue(ctx context.Context, actor *Actor, courseID int64, idempotencyKey string) ([]byte, bool, error) {
	requestJSON := fmt.Sprintf(`{"courseId":%d}`, courseID)
	sum := sha256.Sum256([]byte(requestJSON))
	requestHash := hex.EncodeToString(sum[:])

	tx, err := s.db.Write.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, mapDBError(err)
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM courses WHERE tenant_id = ? AND id = ?`,
		actor.TenantID, courseID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, ErrNotFound
		}
		return nil, false, mapDBError(err)
	}

	var storedHash, storedJSON string
	err = tx.QueryRowContext(ctx, `
		SELECT request_hash, response_json FROM bulk_operations
		WHERE tenant_id = ? AND actor_admin_id = ? AND idempotency_key = ?`,
		actor.TenantID, actor.AdminID, idempotencyKey).Scan(&storedHash, &storedJSON)
	if err == nil {
		if storedHash != requestHash {
			return nil, false, ErrIdempotencyConflict
		}
		return []byte(storedJSON), false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, mapDBError(err)
	}

	started := s.now().UTC().Truncate(time.Millisecond)
	operationID, err := newUUID()
	if err != nil {
		return nil, false, err
	}

	type enrollmentRow struct {
		id          int64
		learnerID   int64
		learnerName string
		completion  string
		certID      sql.NullInt64
		serial      sql.NullString
		certStatus  sql.NullString
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT e.id, e.learner_id, l.name, e.completion_status,
		       c.id, c.serial_number, c.status
		FROM enrollments e
		JOIN learners l ON l.tenant_id = e.tenant_id AND l.id = e.learner_id
		LEFT JOIN certificates c ON c.tenant_id = e.tenant_id AND c.enrollment_id = e.id
		WHERE e.tenant_id = ? AND e.course_id = ?
		ORDER BY e.learner_id ASC`, actor.TenantID, courseID)
	if err != nil {
		return nil, false, mapDBError(err)
	}
	var enrollments []enrollmentRow
	for rows.Next() {
		var e enrollmentRow
		if err := rows.Scan(&e.id, &e.learnerID, &e.learnerName, &e.completion,
			&e.certID, &e.serial, &e.certStatus); err != nil {
			rows.Close()
			return nil, false, err
		}
		enrollments = append(enrollments, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, false, mapDBError(err)
	}
	rows.Close()

	result := BulkOperationResult{
		OperationID: operationID,
		CourseID:    courseID,
		Status:      "COMPLETED",
		Total:       len(enrollments),
		Results:     make([]BulkLearnerResult, 0, len(enrollments)),
	}

	issuedAt := formatUTC(started)
	localDateValue := localDate(started, actor.Location)

	for _, e := range enrollments {
		entry := BulkLearnerResult{LearnerID: e.learnerID, LearnerName: e.learnerName}
		switch {
		case e.completion != "COMPLETED":
			entry.Status = "skipped"
			entry.ReasonCode = stringPtr("COURSE_INCOMPLETE")
			result.Skipped++
			result.Results = append(result.Results, entry)

		case e.certID.Valid && e.certStatus.String == "ACTIVE":
			entry.Status = "skipped"
			entry.ReasonCode = stringPtr("ALREADY_CERTIFIED")
			id := e.certID.Int64
			serial := e.serial.String
			entry.CertificateID = &id
			entry.SerialNumber = &serial
			result.Skipped++
			result.Results = append(result.Results, entry)

		case e.certID.Valid && e.certStatus.String == "REVOKED":
			entry.Status = "skipped"
			entry.ReasonCode = stringPtr("CERTIFICATE_REVOKED")
			id := e.certID.Int64
			serial := e.serial.String
			entry.CertificateID = &id
			entry.SerialNumber = &serial
			result.Skipped++
			result.Results = append(result.Results, entry)

		default:
			serial, err := onlineSerial(actor.TenantCode, started)
			if err != nil {
				return nil, false, err
			}
			if _, err := tx.ExecContext(ctx, `SAVEPOINT learner_issue`); err != nil {
				return nil, false, mapDBError(err)
			}
			res, insertErr := tx.ExecContext(ctx, `
				INSERT INTO certificates
					(tenant_id, enrollment_id, course_id, learner_id, serial_number, status,
					 issued_at, issued_local_date, issued_by_admin_id, bulk_operation_id)
				VALUES (?, ?, ?, ?, ?, 'ACTIVE', ?, ?, ?, ?)`,
				actor.TenantID, e.id, courseID, e.learnerID, serial, issuedAt, localDateValue,
				actor.AdminID, operationID)
			if insertErr == nil {
				var certificateID int64
				certificateID, insertErr = res.LastInsertId()
				if insertErr == nil {
					_, insertErr = tx.ExecContext(ctx, `INSERT INTO audit_events
						(tenant_id, certificate_id, actor_admin_id, event_type, occurred_at)
						VALUES (?, ?, ?, 'ISSUED', ?)`,
						actor.TenantID, certificateID, actor.AdminID, issuedAt)
					if insertErr == nil {
						entry.Status = "issued"
						entry.CertificateID = &certificateID
						entry.SerialNumber = &serial
						result.Issued++
					}
				}
			}
			if insertErr != nil {
				if _, rbErr := tx.ExecContext(ctx, `ROLLBACK TO learner_issue`); rbErr != nil {
					return nil, false, mapDBError(rbErr)
				}
				if _, relErr := tx.ExecContext(ctx, `RELEASE learner_issue`); relErr != nil {
					return nil, false, mapDBError(relErr)
				}
				if !isConstraintError(insertErr) {
					return nil, false, mapDBError(insertErr)
				}
				entry.Status = "failed"
				entry.ReasonCode = stringPtr("ISSUANCE_ERROR")
				entry.CertificateID = nil
				entry.SerialNumber = nil
				result.Failed++
			} else {
				if _, err := tx.ExecContext(ctx, `RELEASE learner_issue`); err != nil {
					return nil, false, mapDBError(err)
				}
			}
			result.Results = append(result.Results, entry)
		}
	}

	completed := s.now().UTC().Truncate(time.Millisecond)
	if completed.Before(started) {
		completed = started
	}
	responseJSON, err := json.Marshal(result)
	if err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO bulk_operations
			(id, tenant_id, actor_admin_id, course_id, idempotency_key, request_hash,
			 request_json, response_json, started_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		operationID, actor.TenantID, actor.AdminID, courseID, idempotencyKey,
		requestHash, requestJSON, string(responseJSON), formatUTC(started), formatUTC(completed)); err != nil {
		return nil, false, mapDBError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, false, mapDBError(err)
	}
	return responseJSON, true, nil
}

func stringPtr(value string) *string {
	return &value
}

func isConstraintError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "constraint failed") ||
		strings.Contains(strings.ToLower(err.Error()), "constraint violation")
}
