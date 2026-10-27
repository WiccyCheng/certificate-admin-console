package store

import (
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"certadmin/migrations"
)

// Migrate applies embedded .sql migrations exactly once each.
func (db *DB) Migrate() error {
	_, err := db.Write.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	) STRICT`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	for i, name := range names {
		version := i + 1
		var applied int
		if err := db.Write.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied > 0 {
			continue
		}
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		tx, err := db.Write.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}
		for _, stmt := range splitStatements(string(body)) {
			if _, err := tx.Exec(stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("apply migration %s: %w", name, err)
			}
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			version, time.Now().UTC().Format("2006-01-02T15:04:05.000Z")); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return nil
}

func splitStatements(body string) []string {
	cleaned := stripLineComments(body)
	parts := strings.Split(cleaned, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, strings.TrimSpace(part))
		}
	}
	return out
}

// stripLineComments removes -- comments outside quoted strings so semicolons in
// comments never split a statement.
func stripLineComments(body string) string {
	var out strings.Builder
	inSingle, inDouble := false, false
	for i := 0; i < len(body); i++ {
		ch := body[i]
		switch {
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
			out.WriteByte(ch)
		case ch == '"' && !inSingle:
			inDouble = !inDouble
			out.WriteByte(ch)
		case ch == '-' && !inSingle && !inDouble && i+1 < len(body) && body[i+1] == '-':
			for i < len(body) && body[i] != '\n' {
				i++
			}
			if i < len(body) {
				out.WriteByte('\n')
			}
		default:
			out.WriteByte(ch)
		}
	}
	return out.String()
}

// SeedIfEmpty initializes the default demo data only when no tenant exists.
// Repeated startups therefore never reset user data.
func (db *DB) SeedIfEmpty() (bool, error) {
	var tenants int
	if err := db.Write.QueryRow(`SELECT COUNT(*) FROM tenants`).Scan(&tenants); err != nil {
		return false, fmt.Errorf("count tenants: %w", err)
	}
	if tenants > 0 {
		return false, nil
	}
	tx, err := db.Write.Begin()
	if err != nil {
		return false, fmt.Errorf("begin seed: %w", err)
	}
	if err := seedDefault(tx); err != nil {
		tx.Rollback()
		return false, fmt.Errorf("seed demo data: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit seed: %w", err)
	}
	return true, nil
}

func seedDefault(tx *sql.Tx) error {
	const insertTenant = `INSERT INTO tenants (id, code, name, timezone) VALUES (?, ?, ?, ?)`
	if _, err := tx.Exec(insertTenant, 1, "EAST", "East", "Australia/Sydney"); err != nil {
		return err
	}
	if _, err := tx.Exec(insertTenant, 2, "WEST", "West", "Australia/Perth"); err != nil {
		return err
	}

	const insertAdmin = `INSERT INTO admins (id, tenant_id, email) VALUES (?, ?, ?)`
	if _, err := tx.Exec(insertAdmin, 1, 1, "alice@east.test"); err != nil {
		return err
	}
	if _, err := tx.Exec(insertAdmin, 2, 2, "bob@west.test"); err != nil {
		return err
	}

	learners := []struct {
		id       int64
		tenantID int64
		name     string
	}{
		{1, 1, "Alex Lee"}, {2, 1, "Sam Chen"}, {3, 1, "Taylor Kim"}, {4, 1, "Morgan Wu"},
		{5, 2, "Alex Lee"}, {6, 2, "Sam Chen"}, {7, 2, "Taylor Kim"}, {8, 2, "Morgan Wu"},
	}
	for _, l := range learners {
		if _, err := tx.Exec(`INSERT INTO learners (id, tenant_id, name) VALUES (?, ?, ?)`, l.id, l.tenantID, l.name); err != nil {
			return err
		}
	}

	courses := []struct {
		id       int64
		tenantID int64
		title    string
	}{
		{1, 1, "Safety 101"}, {2, 1, "First Aid"},
		{3, 2, "Safety 101"}, {4, 2, "First Aid"},
	}
	for _, c := range courses {
		if _, err := tx.Exec(`INSERT INTO courses (id, tenant_id, title) VALUES (?, ?, ?)`, c.id, c.tenantID, c.title); err != nil {
			return err
		}
	}

	type enrollment struct {
		id        int64
		tenantID  int64
		courseID  int64
		learnerID int64
		status    string
		completed string
	}
	enrollments := []enrollment{
		{1, 1, 1, 1, "COMPLETED", "2026-01-09T00:00:00.000Z"},
		{2, 1, 1, 2, "INCOMPLETE", ""},
		{3, 1, 1, 3, "COMPLETED", "2026-01-08T00:00:00.000Z"},
		{4, 1, 1, 4, "COMPLETED", "2026-01-07T00:00:00.000Z"},
		{5, 1, 2, 1, "COMPLETED", "2026-01-09T00:00:00.000Z"},
		{6, 2, 3, 5, "COMPLETED", "2026-01-09T00:00:00.000Z"},
		{7, 2, 3, 6, "INCOMPLETE", ""},
		{8, 2, 3, 7, "COMPLETED", "2026-01-08T00:00:00.000Z"},
		{9, 2, 3, 8, "COMPLETED", "2026-01-07T00:00:00.000Z"},
		{10, 2, 4, 5, "COMPLETED", "2026-01-09T00:00:00.000Z"},
	}
	for _, e := range enrollments {
		var completed any
		if e.completed != "" {
			completed = e.completed
		}
		if _, err := tx.Exec(`INSERT INTO enrollments
			(id, tenant_id, course_id, learner_id, completion_status, completed_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			e.id, e.tenantID, e.courseID, e.learnerID, e.status, completed); err != nil {
			return err
		}
	}

	type certificate struct {
		id         int64
		tenantID   int64
		enrollment int64
		courseID   int64
		learnerID  int64
		serial     string
		status     string
		issuedAt   string
		localDate  string
		issuedBy   int64
		revokedAt  any
		revokedBy  any
	}
	certificates := []certificate{
		{1, 2, 8, 3, 7, "CERT-WEST-TAYLOR-0001", "ACTIVE", "2026-01-10T13:30:00.000Z", "2026-01-10", 2, nil, nil},
		{2, 2, 9, 3, 8, "CERT-WEST-MORGAN-0001", "REVOKED", "2026-01-09T13:30:00.000Z", "2026-01-09", 2, "2026-01-10T00:00:00.000Z", 2},
		{3, 1, 3, 1, 3, "CERT-EAST-TAYLOR-0001", "ACTIVE", "2026-01-10T13:30:00.000Z", "2026-01-11", 1, nil, nil},
		{4, 1, 4, 1, 4, "CERT-EAST-MORGAN-0001", "REVOKED", "2026-01-09T13:30:00.000Z", "2026-01-10", 1, "2026-01-10T00:00:00.000Z", 1},
	}
	for _, c := range certificates {
		if _, err := tx.Exec(`INSERT INTO certificates
			(id, tenant_id, enrollment_id, course_id, learner_id, serial_number, status,
			 issued_at, issued_local_date, issued_by_admin_id, revoked_at, revoked_by_admin_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.id, c.tenantID, c.enrollment, c.courseID, c.learnerID, c.serial, c.status,
			c.issuedAt, c.localDate, c.issuedBy, c.revokedAt, c.revokedBy); err != nil {
			return err
		}
	}

	type audit struct {
		id        int64
		tenantID  int64
		certID    int64
		actorID   int64
		eventType string
		occurred  string
	}
	audits := []audit{
		{1, 2, 1, 2, "ISSUED", "2026-01-10T13:30:00.000Z"},
		{2, 2, 2, 2, "ISSUED", "2026-01-09T13:30:00.000Z"},
		{3, 2, 2, 2, "REVOKED", "2026-01-10T00:00:00.000Z"},
		{4, 1, 3, 1, "ISSUED", "2026-01-10T13:30:00.000Z"},
		{5, 1, 4, 1, "ISSUED", "2026-01-09T13:30:00.000Z"},
		{6, 1, 4, 1, "REVOKED", "2026-01-10T00:00:00.000Z"},
	}
	for _, a := range audits {
		if _, err := tx.Exec(`INSERT INTO audit_events
			(id, tenant_id, certificate_id, actor_admin_id, event_type, occurred_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			a.id, a.tenantID, a.certID, a.actorID, a.eventType, a.occurred); err != nil {
			return err
		}
	}
	return nil
}
