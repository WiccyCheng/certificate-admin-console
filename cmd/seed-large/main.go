// Command seed-large builds the separate 100,000-certificate demo database.
// It never touches data/demo.db and refuses to overwrite an existing file.
package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"certadmin/internal/store"
)

const (
	baseIssuedAt = "2026-01-01T13:30:00.000Z"
	completedAt  = "2025-12-01T00:00:00.000Z"
	batchSize    = 2000
)

type manifest struct {
	DatasetVersion   int               `json:"datasetVersion"`
	Parameters       map[string]any    `json:"parameters"`
	TableCounts      map[string]int64  `json:"tableCounts"`
	TenantCerts      map[string]int64  `json:"tenantCertificates"`
	DateDistribution map[string]int64  `json:"dateDistribution"`
	SearchExpect     map[string]int64  `json:"searchExpectations"`
	Verified         map[string]string `json:"verified"`
}

func main() {
	dbPath := flag.String("db", "", "explicit output database path (required)")
	tenantCode := flag.String("tenant", "east", "tenant code to grow")
	target := flag.Int("certificates", 100000, "target total certificates for the tenant")
	flag.Parse()

	if *dbPath == "" {
		log.Fatal("--db is required")
	}
	abs, err := filepath.Abs(*dbPath)
	if err != nil {
		log.Fatalf("resolve path: %v", err)
	}
	demoAbs, _ := filepath.Abs("data/demo.db")
	if abs == demoAbs {
		log.Fatal("refusing to write the default demo database; choose a separate --db path")
	}
	if _, err := os.Stat(abs); err == nil {
		log.Fatalf("output already exists: %s (choose a new path)", abs)
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("stat output: %v", err)
	}
	if *target < 2 {
		log.Fatal("--certificates must be at least 2 (the default East tenant already has 2)")
	}

	db, err := store.Open(abs)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		db.Close()
		os.Remove(abs)
		log.Fatalf("migrate: %v", err)
	}
	if _, err := db.SeedIfEmpty(); err != nil {
		db.Close()
		os.Remove(abs)
		log.Fatalf("seed default data: %v", err)
	}
	if err := generate(db, *tenantCode, *target); err != nil {
		db.Close()
		removeDatabase(abs)
		log.Fatalf("generate: %v", err)
	}

	output, err := buildManifest(db, *tenantCode, *target)
	if err != nil {
		db.Close()
		removeDatabase(abs)
		log.Fatalf("verify: %v", err)
	}
	manifestPath := strings.TrimSuffix(abs, filepath.Ext(abs)) + ".manifest.json"
	body, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		log.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, append(body, '\n'), 0o644); err != nil {
		log.Fatalf("write manifest: %v", err)
	}
	fmt.Printf("generated %s\nmanifest %s\n", abs, manifestPath)
	pretty, _ := json.MarshalIndent(output, "", "  ")
	fmt.Println(string(pretty))
}

func removeDatabase(path string) {
	os.Remove(path)
	os.Remove(path + "-wal")
	os.Remove(path + "-shm")
}

func generate(db *store.DB, tenantCode string, target int) error {
	var (
		tenantID   int64
		storedCode string
		timezone   string
	)
	if err := db.Write.QueryRow(`SELECT id, code, timezone FROM tenants WHERE lower(code) = lower(?)`,
		tenantCode).Scan(&tenantID, &storedCode, &timezone); err != nil {
		return fmt.Errorf("load tenant %q: %w", tenantCode, err)
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return err
	}
	var adminID int64
	if err := db.Write.QueryRow(`SELECT id FROM admins WHERE tenant_id = ? ORDER BY id LIMIT 1`, tenantID).Scan(&adminID); err != nil {
		return fmt.Errorf("load admin: %w", err)
	}

	var existing int64
	if err := db.Write.QueryRow(`SELECT COUNT(*) FROM certificates WHERE tenant_id = ?`, tenantID).Scan(&existing); err != nil {
		return err
	}
	if int64(target) < existing {
		return fmt.Errorf("target %d is below existing tenant certificate count %d", target, existing)
	}
	add := int64(target) - existing
	if add == 0 {
		return nil
	}

	var (
		baseLearner, baseEnrollment, baseCertificate, baseAudit int64
		courseID                                                int64
	)
	if err := db.Write.QueryRow(`SELECT COALESCE(MAX(id),0) FROM learners`).Scan(&baseLearner); err != nil {
		return err
	}
	if err := db.Write.QueryRow(`SELECT COALESCE(MAX(id),0) FROM enrollments`).Scan(&baseEnrollment); err != nil {
		return err
	}
	if err := db.Write.QueryRow(`SELECT COALESCE(MAX(id),0) FROM certificates`).Scan(&baseCertificate); err != nil {
		return err
	}
	if err := db.Write.QueryRow(`SELECT COALESCE(MAX(id),0) FROM audit_events`).Scan(&baseAudit); err != nil {
		return err
	}
	if err := db.Write.QueryRow(`SELECT COALESCE(MAX(id),0)+1 FROM courses`).Scan(&courseID); err != nil {
		return err
	}

	if _, err := db.Write.Exec(`INSERT INTO courses (id, tenant_id, title) VALUES (?, ?, 'Export Load')`,
		courseID, tenantID); err != nil {
		return err
	}

	baseTime, err := time.Parse("2006-01-02T15:04:05.000Z", baseIssuedAt)
	if err != nil {
		return err
	}

	started := time.Now()
	for start := int64(1); start <= add; start += batchSize {
		end := start + batchSize - 1
		if end > add {
			end = add
		}
		tx, err := db.Write.Begin()
		if err != nil {
			return err
		}
		learnerStmt, err := tx.Prepare(`INSERT INTO learners (id, tenant_id, name) VALUES (?, ?, ?)`)
		if err != nil {
			tx.Rollback()
			return err
		}
		enrollmentStmt, err := tx.Prepare(`INSERT INTO enrollments
			(id, tenant_id, course_id, learner_id, completion_status, completed_at)
			VALUES (?, ?, ?, ?, 'COMPLETED', ?)`)
		if err != nil {
			tx.Rollback()
			return err
		}
		certificateStmt, err := tx.Prepare(`INSERT INTO certificates
			(id, tenant_id, enrollment_id, course_id, learner_id, serial_number, status,
			 issued_at, issued_local_date, issued_by_admin_id)
			VALUES (?, ?, ?, ?, ?, ?, 'ACTIVE', ?, ?, ?)`)
		if err != nil {
			tx.Rollback()
			return err
		}
		auditStmt, err := tx.Prepare(`INSERT INTO audit_events
			(id, tenant_id, certificate_id, actor_admin_id, event_type, occurred_at)
			VALUES (?, ?, ?, ?, 'ISSUED', ?)`)
		if err != nil {
			tx.Rollback()
			return err
		}

		for i := start; i <= end; i++ {
			learnerID := baseLearner + i
			enrollmentID := baseEnrollment + i
			certificateID := baseCertificate + i
			auditID := baseAudit + i
			name := fmt.Sprintf("Load Learner %03d", i%100)
			serial := fmt.Sprintf("%s-LOAD-%06d", strings.ToUpper(storedCode), i)
			issued := baseTime.AddDate(0, 0, int(i%30))
			issuedAt := issued.Format("2006-01-02T15:04:05.000Z")
			localDate := issued.In(loc).Format("2006-01-02")

			if _, err := learnerStmt.Exec(learnerID, tenantID, name); err != nil {
				tx.Rollback()
				return fmt.Errorf("learner %d: %w", i, err)
			}
			if _, err := enrollmentStmt.Exec(enrollmentID, tenantID, courseID, learnerID, completedAt); err != nil {
				tx.Rollback()
				return fmt.Errorf("enrollment %d: %w", i, err)
			}
			if _, err := certificateStmt.Exec(certificateID, tenantID, enrollmentID, courseID, learnerID,
				serial, issuedAt, localDate, adminID); err != nil {
				tx.Rollback()
				return fmt.Errorf("certificate %d: %w", i, err)
			}
			if _, err := auditStmt.Exec(auditID, tenantID, certificateID, adminID, issuedAt); err != nil {
				tx.Rollback()
				return fmt.Errorf("audit %d: %w", i, err)
			}
		}
		learnerStmt.Close()
		enrollmentStmt.Close()
		certificateStmt.Close()
		auditStmt.Close()
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	log.Printf("generated %d certificates in %s", add, time.Since(started).Round(time.Millisecond))
	return nil
}

func buildManifest(db *store.DB, tenantCode string, target int) (manifest, error) {
	out := manifest{
		DatasetVersion:   1,
		Parameters:       map[string]any{"tenant": strings.ToLower(tenantCode), "certificates": target, "batchSize": batchSize},
		TableCounts:      map[string]int64{},
		TenantCerts:      map[string]int64{},
		DateDistribution: map[string]int64{},
		SearchExpect:     map[string]int64{},
		Verified:         map[string]string{},
	}

	for _, table := range []string{"tenants", "admins", "learners", "courses", "enrollments", "certificates", "bulk_operations", "audit_events"} {
		var count int64
		if err := db.Read.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			return out, err
		}
		out.TableCounts[table] = count
	}
	rows, err := db.Read.Query(`SELECT code, COUNT(*) FROM certificates c JOIN tenants t ON t.id = c.tenant_id GROUP BY code ORDER BY code`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var code string
		var count int64
		if err := rows.Scan(&code, &count); err != nil {
			rows.Close()
			return out, err
		}
		out.TenantCerts[code] = count
	}
	rows.Close()

	var tenantID int64
	if err := db.Read.QueryRow(`SELECT id FROM tenants WHERE lower(code) = lower(?)`, tenantCode).Scan(&tenantID); err != nil {
		return out, err
	}
	dateRows, err := db.Read.Query(`
		SELECT issued_local_date, COUNT(*) FROM certificates
		WHERE tenant_id = ? GROUP BY issued_local_date ORDER BY issued_local_date`, tenantID)
	if err != nil {
		return out, err
	}
	for dateRows.Next() {
		var date string
		var count int64
		if err := dateRows.Scan(&date, &count); err != nil {
			dateRows.Close()
			return out, err
		}
		out.DateDistribution[date] = count
	}
	dateRows.Close()

	var exportCourseID int64
	err = db.Read.QueryRow(`SELECT id FROM courses WHERE tenant_id = ? AND title = 'Export Load'`, tenantID).Scan(&exportCourseID)
	if err == nil {
		var certs, enrollments, audits, active int64
		if err := db.Read.QueryRow(`SELECT COUNT(*), SUM(CASE WHEN status = 'ACTIVE' THEN 1 ELSE 0 END)
			FROM certificates WHERE tenant_id = ? AND course_id = ?`, tenantID, exportCourseID).Scan(&certs, &active); err != nil {
			return out, err
		}
		if err := db.Read.QueryRow(`SELECT COUNT(*) FROM enrollments WHERE tenant_id = ? AND course_id = ?`,
			tenantID, exportCourseID).Scan(&enrollments); err != nil {
			return out, err
		}
		if err := db.Read.QueryRow(`SELECT COUNT(*) FROM audit_events a
			JOIN certificates c ON c.tenant_id = a.tenant_id AND c.id = a.certificate_id
			WHERE c.tenant_id = ? AND c.course_id = ? AND a.event_type = 'ISSUED'`,
			tenantID, exportCourseID).Scan(&audits); err != nil {
			return out, err
		}
		if certs != enrollments || audits != certs || active != certs {
			return out, fmt.Errorf("Export Load counts differ: certs=%d enrollments=%d audits=%d active=%d",
				certs, enrollments, audits, active)
		}
		out.Verified["exportLoadConsistency"] = fmt.Sprintf("certs=%d enrollments=%d audits=%d active=%d",
			certs, enrollments, audits, active)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}

	var nameMatches, serialMatches int64
	if err := db.Read.QueryRow(`
		SELECT COUNT(*) FROM certificates c
		JOIN learners l ON l.tenant_id = c.tenant_id AND l.id = c.learner_id
		WHERE c.tenant_id = ? AND l.name = 'Load Learner 007'`, tenantID).Scan(&nameMatches); err != nil {
		return out, err
	}
	serialExample := strings.ToUpper(tenantCode) + "-LOAD-000007"
	if err := db.Read.QueryRow(`SELECT COUNT(*) FROM certificates WHERE tenant_id = ? AND serial_number = ?`,
		tenantID, serialExample).Scan(&serialMatches); err != nil {
		return out, err
	}
	out.SearchExpect["learnerName=Load Learner 007"] = nameMatches
	out.SearchExpect["serialNumber="+serialExample] = serialMatches

	var foreignKeyIssues int
	fkRows, err := db.Read.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return out, err
	}
	for fkRows.Next() {
		foreignKeyIssues++
	}
	fkRows.Close()
	if foreignKeyIssues != 0 {
		return out, fmt.Errorf("foreign_key_check found %d issues", foreignKeyIssues)
	}
	out.Verified["foreignKeyCheck"] = "ok"

	var integrity string
	if err := db.Read.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return out, err
	}
	if integrity != "ok" {
		return out, fmt.Errorf("integrity_check: %s", integrity)
	}
	out.Verified["integrityCheck"] = integrity

	var missingIssuedAudit int64
	if err := db.Read.QueryRow(`
		SELECT COUNT(*) FROM certificates c
		WHERE c.tenant_id = ?
		  AND NOT EXISTS (
		    SELECT 1 FROM audit_events a
		    WHERE a.tenant_id = c.tenant_id AND a.certificate_id = c.id AND a.event_type = 'ISSUED'
		  )`, tenantID).Scan(&missingIssuedAudit); err != nil {
		return out, err
	}
	if missingIssuedAudit != 0 {
		return out, fmt.Errorf("%d certificates missing ISSUED audit", missingIssuedAudit)
	}
	var extraIssuedAudit int64
	if err := db.Read.QueryRow(`
		SELECT COUNT(*) FROM audit_events a
		WHERE a.tenant_id = ? AND a.event_type = 'ISSUED'
		  AND NOT EXISTS (SELECT 1 FROM certificates c WHERE c.tenant_id = a.tenant_id AND c.id = a.certificate_id)`,
		tenantID).Scan(&extraIssuedAudit); err != nil {
		return out, err
	}
	if extraIssuedAudit != 0 {
		return out, fmt.Errorf("%d ISSUED audits without certificates", extraIssuedAudit)
	}
	if out.TenantCerts[strings.ToUpper(tenantCode)] != int64(target) {
		return out, fmt.Errorf("tenant certificate count = %d, want %d", out.TenantCerts[strings.ToUpper(tenantCode)], target)
	}
	out.Verified["issuedAuditConsistency"] = "ok"
	out.Verified["targetCertificateCount"] = "ok"
	return out, nil
}
