// Package database opens the FloatTranslate SQLite database (pure-Go
// modernc.org/sqlite, no cgo) with the pragmas mandated by docs/05: WAL
// journal mode and foreign keys ON.
package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Open opens (creating if necessary) the SQLite database file at path.
//
// The DSN enables the pragmas required by the data model:
//   - journal_mode = WAL
//   - foreign_keys = ON (needed for chats→messages ON DELETE CASCADE)
//   - busy_timeout = 5000ms
//
// The pool is capped at a single connection: the local sidecar is a
// low-volume single-user workload, and one connection removes all
// SQLITE_BUSY contention between readers and the WAL writer. Callers must
// therefore never keep rows open while issuing further statements.
func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return db, nil
}
