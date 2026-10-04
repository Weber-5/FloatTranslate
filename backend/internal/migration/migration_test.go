package migration

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
	"github.com/Weber-5/FloatTranslate/backend/internal/database"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

var expectedTables = []string{
	"app_meta", "settings", "translation_cache", "translation_history",
	"vocabulary", "terminology", "chats", "messages", "open_tabs",
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var one int
	err := db.QueryRow(
		`SELECT 1 FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&one)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("check table %s: %v", name, err)
	}
	return true
}

func TestMigrationFreshDB(t *testing.T) {
	db := openTestDB(t)
	if err := Run(db, Embedded()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, table := range expectedTables {
		if !tableExists(t, db, table) {
			t.Errorf("expected table %s after migration", table)
		}
	}
	var version string
	if err := db.QueryRow(`SELECT value FROM app_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if version != "1" {
		t.Errorf("schema_version = %q, want 1", version)
	}
}

func TestMigrationIdempotent(t *testing.T) {
	db := openTestDB(t)
	if err := Run(db, Embedded()); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Run(db, Embedded()); err != nil {
		t.Fatalf("second migrate must be a no-op: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != 1 {
		t.Errorf("schema_migrations rows = %d, want 1", count)
	}
}

func TestMigrationFailureSurfacesMigrationFailed(t *testing.T) {
	db := openTestDB(t)
	bad := []Migration{{Version: 1, Name: "broken", SQL: `CREATE TABLE definitely_broken (`}}
	err := Run(db, bad)
	if err == nil {
		t.Fatal("expected migration failure")
	}
	e := apperr.AsE(err)
	if e.Code != apperr.CodeMigrationFailed {
		t.Errorf("code = %s, want MIGRATION_FAILED", e.Code)
	}
	if e.Retryable {
		t.Errorf("MIGRATION_FAILED must not be retryable")
	}
	// The failed migration rolled back: its table must not exist.
	if tableExists(t, db, "definitely_broken") {
		t.Errorf("failed migration was not rolled back")
	}
	// The DB stays usable: a valid migration can still be applied afterwards.
	if err := Run(db, Embedded()); err != nil {
		t.Fatalf("recover with valid migration: %v", err)
	}
	if !tableExists(t, db, "settings") {
		t.Errorf("expected settings table after recovery")
	}
}

// TestMigrationFreshDBReopenFixture is the startup fixture: a database that
// has never seen schema_migrations migrates cleanly, survives a close/reopen
// cycle with its data intact, and a second migration run on the reopened
// database is a no-op.
func TestMigrationFreshDBReopenFixture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.db")

	// First launch: raw v0 database (the file does not exist yet).
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var one int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&one); err != nil || one != 0 {
		_ = db.Close()
		t.Fatalf("fixture must start without schema_migrations (count=%d err=%v)", one, err)
	}
	if err := Run(db, Embedded()); err != nil {
		_ = db.Close()
		t.Fatalf("first migrate: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO settings(key, value_json, updated_at) VALUES('theme', '"dark"', '2026-10-04T00:00:00Z')`,
	); err != nil {
		_ = db.Close()
		t.Fatalf("seed row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Relaunch: re-open and migrate again — idempotent, data survives.
	db2, err := database.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
	if err := Run(db2, Embedded()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var count int
	if err := db2.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != 1 {
		t.Errorf("schema_migrations rows = %d, want 1", count)
	}
	var theme string
	if err := db2.QueryRow(`SELECT value_json FROM settings WHERE key='theme'`).Scan(&theme); err != nil {
		t.Fatalf("seeded row lost across reopen: %v", err)
	}
	if theme != `"dark"` {
		t.Errorf("theme = %s", theme)
	}
	var version string
	if err := db2.QueryRow(`SELECT value FROM app_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != "1" {
		t.Errorf("schema_version = %q err=%v, want 1", version, err)
	}
}
