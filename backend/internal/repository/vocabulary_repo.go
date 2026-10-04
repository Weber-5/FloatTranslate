package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// VocabularyRepo provides access to the vocabulary table (docs/05 §2).
// Entries are deduplicated by lemma (COLLATE NOCASE): re-saving an existing
// lemma only refreshes last_viewed_at.
type VocabularyRepo struct {
	db *sql.DB
}

// NewVocabularyRepo builds a VocabularyRepo on db.
func NewVocabularyRepo(db *sql.DB) *VocabularyRepo { return &VocabularyRepo{db: db} }

// VocabularyRow is one saved word.
type VocabularyRow struct {
	Lemma        string
	WordJSON     string
	SavedAt      string
	LastViewedAt string
}

// Upsert saves the word; when the lemma (case-insensitive) already exists
// only last_viewed_at is refreshed — saved_at and word_json are kept.
func (r *VocabularyRepo) Upsert(ctx context.Context, row VocabularyRow) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO vocabulary(lemma, word_json, saved_at, last_viewed_at)
		 VALUES(?, ?, ?, ?)
		 ON CONFLICT(lemma) DO UPDATE SET last_viewed_at = excluded.last_viewed_at`,
		row.Lemma, row.WordJSON, row.SavedAt, row.LastViewedAt,
	)
	if err != nil {
		return fmt.Errorf("vocabulary upsert: %w", err)
	}
	return nil
}

// Get returns the saved word for lemma (case-insensitive), or ErrNotFound.
func (r *VocabularyRepo) Get(ctx context.Context, lemma string) (VocabularyRow, error) {
	var row VocabularyRow
	err := r.db.QueryRowContext(ctx,
		`SELECT lemma, word_json, saved_at, last_viewed_at FROM vocabulary WHERE lemma = ?`, lemma,
	).Scan(&row.Lemma, &row.WordJSON, &row.SavedAt, &row.LastViewedAt)
	if err == sql.ErrNoRows {
		return VocabularyRow{}, ErrNotFound
	}
	if err != nil {
		return VocabularyRow{}, fmt.Errorf("vocabulary get: %w", err)
	}
	return row, nil
}

// List returns saved words ordered by saved_at DESC (newest first),
// optionally filtered by a case-insensitive substring match on lemma.
func (r *VocabularyRepo) List(ctx context.Context, query string) ([]VocabularyRow, error) {
	q := `SELECT lemma, word_json, saved_at, last_viewed_at FROM vocabulary`
	var args []any
	if query != "" {
		q += ` WHERE lemma LIKE ? ESCAPE '\' COLLATE NOCASE`
		args = append(args, "%"+likeEscape(query)+"%")
	}
	q += ` ORDER BY saved_at DESC, lemma COLLATE NOCASE`
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("vocabulary list: %w", err)
	}
	defer rows.Close()
	var out []VocabularyRow
	for rows.Next() {
		var row VocabularyRow
		if err := rows.Scan(&row.Lemma, &row.WordJSON, &row.SavedAt, &row.LastViewedAt); err != nil {
			return nil, fmt.Errorf("vocabulary list scan: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// Delete removes a saved word. Returns ErrNotFound when missing.
func (r *VocabularyRepo) Delete(ctx context.Context, lemma string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM vocabulary WHERE lemma = ?`, lemma)
	if err != nil {
		return fmt.Errorf("vocabulary delete: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}
