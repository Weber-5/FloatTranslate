package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// TerminologyRepo provides access to the terminology table (docs/05 §2).
// source is case-insensitively unique.
type TerminologyRepo struct {
	db *sql.DB
}

// NewTerminologyRepo builds a TerminologyRepo on db.
func NewTerminologyRepo(db *sql.DB) *TerminologyRepo { return &TerminologyRepo{db: db} }

// TerminologyRow is one terminology entry.
type TerminologyRow struct {
	ID        string
	Source    string
	Target    string
	CreatedAt string
	UpdatedAt string
}

// List returns all terminology entries ordered by source (case-insensitive).
func (r *TerminologyRepo) List(ctx context.Context) ([]TerminologyRow, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, source, target, created_at, updated_at
		 FROM terminology ORDER BY source COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("terminology list: %w", err)
	}
	defer rows.Close()
	var out []TerminologyRow
	for rows.Next() {
		var row TerminologyRow
		if err := rows.Scan(&row.ID, &row.Source, &row.Target, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, fmt.Errorf("terminology list scan: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// Get returns the entry with id, or ErrNotFound.
func (r *TerminologyRepo) Get(ctx context.Context, id string) (TerminologyRow, error) {
	var row TerminologyRow
	err := r.db.QueryRowContext(ctx,
		`SELECT id, source, target, created_at, updated_at FROM terminology WHERE id = ?`, id,
	).Scan(&row.ID, &row.Source, &row.Target, &row.CreatedAt, &row.UpdatedAt)
	if err == sql.ErrNoRows {
		return TerminologyRow{}, ErrNotFound
	}
	if err != nil {
		return TerminologyRow{}, fmt.Errorf("terminology get: %w", err)
	}
	return row, nil
}

// sourceExists reports whether another row already uses source
// (case-insensitive), optionally excluding id.
func (r *TerminologyRepo) sourceExists(ctx context.Context, source, excludeID string) (bool, error) {
	q := `SELECT 1 FROM terminology WHERE source = ?`
	args := []any{source}
	if excludeID != "" {
		q += ` AND id <> ?`
		args = append(args, excludeID)
	}
	var one int
	err := r.db.QueryRowContext(ctx, q, args...).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("terminology source exists: %w", err)
	}
	return true, nil
}

// Create inserts a new entry. Returns ErrConflict when source already exists
// (case-insensitive).
func (r *TerminologyRepo) Create(ctx context.Context, row TerminologyRow) error {
	exists, err := r.sourceExists(ctx, row.Source, "")
	if err != nil {
		return err
	}
	if exists {
		return ErrConflict
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO terminology(id, source, target, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
		row.ID, row.Source, row.Target, row.CreatedAt, row.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("terminology create: %w", err)
	}
	return nil
}

// Update modifies source/target of an entry. Returns ErrNotFound when the id
// does not exist and ErrConflict when the new source collides with another row.
func (r *TerminologyRepo) Update(ctx context.Context, row TerminologyRow) error {
	exists, err := r.sourceExists(ctx, row.Source, row.ID)
	if err != nil {
		return err
	}
	if exists {
		return ErrConflict
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE terminology SET source = ?, target = ?, updated_at = ? WHERE id = ?`,
		row.Source, row.Target, row.UpdatedAt, row.ID,
	)
	if err != nil {
		return fmt.Errorf("terminology update: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes an entry. Returns ErrNotFound when missing.
func (r *TerminologyRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM terminology WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("terminology delete: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}
