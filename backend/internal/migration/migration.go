// Package migration applies versioned schema migrations to the SQLite
// database (docs/05 §1): forward-only, applied inside a transaction each,
// tracked in schema_migrations, idempotent on re-run. Any failure aborts the
// failing migration and surfaces a MIGRATION_FAILED application error — the
// server must refuse to start and no business writes may happen.
package migration

import (
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
)

//go:embed v1.sql
var v1SQL string

// Migration is a single forward schema migration.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Embedded returns the migrations bundled with the binary, in order.
func Embedded() []Migration {
	return []Migration{
		{Version: 1, Name: "initial_schema", SQL: v1SQL},
	}
}

const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    applied_at TEXT NOT NULL
);`

// Run applies every not-yet-applied migration in order. Each migration runs
// in its own transaction; on failure the transaction is rolled back and a
// MIGRATION_FAILED *apperr.E is returned. Running with everything already
// applied is a no-op (idempotent).
func Run(db *sql.DB, migrations []Migration) error {
	return run(db, migrations, nil)
}

func run(db *sql.DB, migrations []Migration, now func() time.Time) error {
	if now == nil {
		now = time.Now
	}
	if _, err := db.Exec(createMigrationsTable); err != nil {
		return apperr.Wrap(apperr.CodeMigrationFailed, "数据库迁移失败", false, fmt.Errorf("create schema_migrations: %w", err))
	}
	for _, m := range migrations {
		applied, err := isApplied(db, m.Version)
		if err != nil {
			return apperr.Wrap(apperr.CodeMigrationFailed, "数据库迁移失败", false, err)
		}
		if applied {
			continue
		}
		if err := apply(db, m, now().UTC().Format(time.RFC3339)); err != nil {
			return apperr.Wrap(apperr.CodeMigrationFailed, "数据库迁移失败", false, err)
		}
	}
	if len(migrations) > 0 {
		latest := migrations[len(migrations)-1].Version
		if _, err := db.Exec(
			`INSERT INTO app_meta(key, value) VALUES('schema_version', ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, fmt.Sprintf("%d", latest),
		); err != nil {
			return apperr.Wrap(apperr.CodeMigrationFailed, "数据库迁移失败", false, fmt.Errorf("record schema_version: %w", err))
		}
	}
	return nil
}

func isApplied(db *sql.DB, version int) (bool, error) {
	var one int
	err := db.QueryRow(`SELECT 1 FROM schema_migrations WHERE version = ?`, version).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check migration %d: %w", version, err)
	}
	return true, nil
}

func apply(db *sql.DB, m Migration, appliedAt string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", m.Version, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(m.SQL); err != nil {
		return fmt.Errorf("apply migration %d (%s): %w", m.Version, m.Name, err)
	}
	if _, err := tx.Exec(
		`INSERT INTO schema_migrations(version, name, applied_at) VALUES(?, ?, ?)`,
		m.Version, m.Name, appliedAt,
	); err != nil {
		return fmt.Errorf("record migration %d: %w", m.Version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", m.Version, err)
	}
	return nil
}
