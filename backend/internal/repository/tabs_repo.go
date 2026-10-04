package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// TabsRepo provides access to open_tabs (docs/05 §2). Phase 1 uses simple
// full replacement on every PUT.
type TabsRepo struct {
	db *sql.DB
}

// NewTabsRepo builds a TabsRepo on db.
func NewTabsRepo(db *sql.DB) *TabsRepo { return &TabsRepo{db: db} }

// TabRow is one persisted tab.
type TabRow struct {
	ID            string
	Kind          string
	Title         string
	TranslationID sql.NullString
	PayloadJSON   string
	Position      int
	IsActive      bool
	UpdatedAt     string
}

// ReplaceAll atomically replaces the persisted tab list with rows.
func (r *TabsRepo) ReplaceAll(ctx context.Context, rows []TabRow) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("tabs replace begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM open_tabs`); err != nil {
		return fmt.Errorf("tabs replace clear: %w", err)
	}
	for _, row := range rows {
		active := 0
		if row.IsActive {
			active = 1
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO open_tabs(id, kind, title, translation_id, payload_json, position, is_active, updated_at)
			 VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
			row.ID, row.Kind, row.Title, row.TranslationID, row.PayloadJSON, row.Position, active, row.UpdatedAt,
		); err != nil {
			return fmt.Errorf("tabs replace insert: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("tabs replace commit: %w", err)
	}
	return nil
}

// DeleteAll clears the whole open_tabs table (clear business / reset app).
func (r *TabsRepo) DeleteAll(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM open_tabs`); err != nil {
		return fmt.Errorf("tabs delete all: %w", err)
	}
	return nil
}

// List returns the persisted tabs ordered by position.
func (r *TabsRepo) List(ctx context.Context) ([]TabRow, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, kind, title, translation_id, payload_json, position, is_active, updated_at
		 FROM open_tabs ORDER BY position`)
	if err != nil {
		return nil, fmt.Errorf("tabs list: %w", err)
	}
	defer rows.Close()
	var out []TabRow
	for rows.Next() {
		var row TabRow
		var active int
		if err := rows.Scan(&row.ID, &row.Kind, &row.Title, &row.TranslationID, &row.PayloadJSON,
			&row.Position, &active, &row.UpdatedAt); err != nil {
			return nil, fmt.Errorf("tabs list scan: %w", err)
		}
		row.IsActive = active != 0
		out = append(out, row)
	}
	return out, rows.Err()
}
