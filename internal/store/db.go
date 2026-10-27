// Package store opens the SQLite database, applies migrations and seeds demo data.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// DB holds one serialized write pool and a concurrent read pool.
type DB struct {
	Write *sql.DB
	Read  *sql.DB
	Path  string
}

// Open creates or opens the SQLite database at path. WAL, foreign keys and a
// busy timeout are configured per connection; reads use a query-only pool.
func Open(path string) (*DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	pragmas := url.Values{}
	pragmas.Add("_pragma", "busy_timeout(10000)")
	pragmas.Add("_pragma", "foreign_keys(1)")
	pragmas.Add("_pragma", "journal_mode(WAL)")
	pragmas.Add("_pragma", "synchronous(NORMAL)")

	writeDSN := "file:" + filepath.ToSlash(abs) + "?" + pragmas.Encode() + "&_txlock=immediate"
	write, err := sql.Open("sqlite", writeDSN)
	if err != nil {
		return nil, fmt.Errorf("open write database: %w", err)
	}
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)
	write.SetConnMaxLifetime(0)
	if err := write.Ping(); err != nil {
		write.Close()
		return nil, fmt.Errorf("ping write database: %w", err)
	}

	readPragmas := url.Values{}
	readPragmas.Add("_pragma", "busy_timeout(10000)")
	readPragmas.Add("_pragma", "foreign_keys(1)")
	readPragmas.Add("_pragma", "query_only(1)")
	readDSN := "file:" + filepath.ToSlash(abs) + "?" + readPragmas.Encode()
	read, err := sql.Open("sqlite", readDSN)
	if err != nil {
		write.Close()
		return nil, fmt.Errorf("open read database: %w", err)
	}
	read.SetMaxOpenConns(8)
	read.SetMaxIdleConns(8)
	read.SetConnMaxLifetime(0)
	if err := read.Ping(); err != nil {
		write.Close()
		read.Close()
		return nil, fmt.Errorf("ping read database: %w", err)
	}

	return &DB{Write: write, Read: read, Path: abs}, nil
}

// Close releases both pools.
func (db *DB) Close() error {
	return errors.Join(db.Read.Close(), db.Write.Close())
}

// IsBusy reports whether err is a transient SQLite lock/busy error.
func IsBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "sqlite_busy") ||
		strings.Contains(msg, "sqlite_locked") ||
		strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked")
}
