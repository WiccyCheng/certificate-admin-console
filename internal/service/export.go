package service

import (
	"context"
	"database/sql"
)

// StreamCertificates runs one consistent read transaction and emits every
// matching row in the same order as the list endpoint. start is called after
// the query has produced its first row but before any row is emitted; if the
// query itself fails, start is never called and the handler can still send a
// JSON error. Errors returned after start must abort the HTTP connection.
func (s *Service) StreamCertificates(
	ctx context.Context,
	tenantID int64,
	f CertificateFilter,
	start func(),
	emit func(CertificateCSVRow) error,
) error {
	where, args := certificateWhere(tenantID, f)
	tx, err := s.db.Read.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return mapDBError(err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT l.name, co.title, c.serial_number, c.issued_local_date
		FROM certificates c
		JOIN learners l ON l.tenant_id = c.tenant_id AND l.id = c.learner_id
		JOIN courses co ON co.tenant_id = c.tenant_id AND co.id = c.course_id
		WHERE `+where+`
		ORDER BY `+certificateOrder(f), args...)
	if err != nil {
		return mapDBError(err)
	}
	defer rows.Close()

	hasRow := rows.Next()
	if err := rows.Err(); err != nil {
		return mapDBError(err)
	}
	start()
	for hasRow {
		var row CertificateCSVRow
		if err := rows.Scan(&row.LearnerName, &row.CourseTitle, &row.SerialNumber, &row.IssuedLocalDate); err != nil {
			return err
		}
		if err := emit(row); err != nil {
			return err
		}
		hasRow = rows.Next()
	}
	return mapDBError(rows.Err())
}
