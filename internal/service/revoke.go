package service

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Revoke creates the singleton revocation record. First ACTIVE -> REVOKED is
// created=true (HTTP 201); an already REVOKED certificate returns its original
// revocation data with created=false (HTTP 200).
func (s *Service) Revoke(ctx context.Context, actor *Actor, certificateID int64) (RevocationResult, bool, error) {
	var out RevocationResult
	tx, err := s.db.Write.BeginTx(ctx, nil)
	if err != nil {
		return out, false, mapDBError(err)
	}
	defer tx.Rollback()

	var (
		status       string
		revokedAt    sql.NullString
		revokedByID  sql.NullInt64
		revokedEmail sql.NullString
	)
	err = tx.QueryRowContext(ctx, `
		SELECT c.status, c.revoked_at, ra.id, ra.email
		FROM certificates c
		LEFT JOIN admins ra ON ra.tenant_id = c.tenant_id AND ra.id = c.revoked_by_admin_id
		WHERE c.tenant_id = ? AND c.id = ?`, actor.TenantID, certificateID).
		Scan(&status, &revokedAt, &revokedByID, &revokedEmail)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, false, ErrNotFound
		}
		return out, false, mapDBError(err)
	}

	if status == "REVOKED" {
		out = RevocationResult{
			CertificateID: certificateID,
			RevokedAt:     revokedAt.String,
			RevokedBy:     AdminRef{ID: revokedByID.Int64, Email: revokedEmail.String},
		}
		return out, false, nil
	}

	now := formatUTC(s.now().UTC().Truncate(time.Millisecond))
	res, err := tx.ExecContext(ctx, `
		UPDATE certificates
		SET status = 'REVOKED', revoked_at = ?, revoked_by_admin_id = ?
		WHERE tenant_id = ? AND id = ? AND status = 'ACTIVE'`,
		now, actor.AdminID, actor.TenantID, certificateID)
	if err != nil {
		return out, false, mapDBError(err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return out, false, mapDBError(err)
	}
	if affected != 1 {
		return out, false, ErrBusy
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events
		(tenant_id, certificate_id, actor_admin_id, event_type, occurred_at)
		VALUES (?, ?, ?, 'REVOKED', ?)`,
		actor.TenantID, certificateID, actor.AdminID, now); err != nil {
		return out, false, mapDBError(err)
	}
	if err := tx.Commit(); err != nil {
		return out, false, mapDBError(err)
	}
	out = RevocationResult{
		CertificateID: certificateID,
		RevokedAt:     now,
		RevokedBy:     AdminRef{ID: actor.AdminID, Email: actor.Email},
	}
	return out, true, nil
}
