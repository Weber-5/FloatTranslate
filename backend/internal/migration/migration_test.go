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
