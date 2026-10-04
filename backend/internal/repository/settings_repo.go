package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SettingsRepo provides key/value JSON access to the settings table
// (docs/05 §2). Only non-secret settings are stored here — API keys never
// enter SQLite.
type SettingsRepo struct {
	db *sql.DB
}

// NewSettingsRepo builds a SettingsRepo on db.
func NewSettingsRepo(db *sql.DB) *SettingsRepo { return &SettingsRepo{db: db} }

// SettingRow is one settings entry.
type SettingRow struct {
	Key       string
	ValueJSON string
	UpdatedAt string
}

// Get returns the raw JSON value stored under key.
func (r *SettingsRepo) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := r.db.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("settings get %q: %w", key, err)
	}
	return value, nil
}

// Put stores (or replaces) the JSON value under key.
func (r *SettingsRepo) Put(ctx context.Context, key, valueJSON string) error {
	if valueJSON == "" {
		valueJSON = "null"
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO settings(key, value_json, updated_at) VALUES(?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`,
		key, valueJSON, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("settings put %q: %w", key, err)
	}
	return nil
}

// DeleteAll wipes the settings table (Reset App). The API key is not stored
// here and is unaffected.
func (r *SettingsRepo) DeleteAll(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM settings`); err != nil {
		return fmt.Errorf("settings delete all: %w", err)
	}
	return nil
}

// List returns every stored setting ordered by key.
func (r *SettingsRepo) List(ctx context.Context) ([]SettingRow, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT key, value_json, updated_at FROM settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("settings list: %w", err)
	}
	defer rows.Close()
	var out []SettingRow
	for rows.Next() {
		var row SettingRow
		if err := rows.Scan(&row.Key, &row.ValueJSON, &row.UpdatedAt); err != nil {
			return nil, fmt.Errorf("settings list scan: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
