package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
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
// the stored word_json and last_viewed_at are updated while saved_at keeps
// the original first-save time (Phase 2 freeze: idempotent upsert of the
// full WordTranslation, no duplicate rows).
func (r *VocabularyRepo) Upsert(ctx context.Context, row VocabularyRow) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO vocabulary(lemma, word_json, saved_at, last_viewed_at)
		 VALUES(?, ?, ?, ?)
		 ON CONFLICT(lemma) DO UPDATE SET
		   word_json = excluded.word_json,
		   last_viewed_at = excluded.last_viewed_at`,
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
// optionally filtered by a case-insensitive substring match on the lemma,
// the word itself and its Chinese meanings (Phase 2: query spans all three).
func (r *VocabularyRepo) List(ctx context.Context, query string) ([]VocabularyRow, error) {
	q := `SELECT lemma, word_json, saved_at, last_viewed_at FROM vocabulary`
	q += ` ORDER BY saved_at DESC, lemma COLLATE NOCASE`
	rows, err := r.db.QueryContext(ctx, q)
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
		if query == "" || vocabularyMatches(row, query) {
			out = append(out, row)
		}
	}
	return out, rows.Err()
}

// vocabularyMatch is the subset of WordTranslation used for query filtering.
type vocabularyMatch struct {
	Word          string `json:"word"`
	Lemma         string `json:"lemma"`
	PartsOfSpeech []struct {
		Meanings []string `json:"meanings"`
	} `json:"parts_of_speech"`
}

// vocabularyMatches reports whether query (case-insensitive) is a substring
// of the lemma, the word or any meaning of the saved entry.
func vocabularyMatches(row VocabularyRow, query string) bool {
	var m vocabularyMatch
	if err := json.Unmarshal([]byte(row.WordJSON), &m); err != nil {
		// Corrupted JSON still allows matching on the lemma itself.
		return containsFold(row.Lemma, query)
	}
	if containsFold(row.Lemma, query) || containsFold(m.Lemma, query) || containsFold(m.Word, query) {
		return true
	}
	for _, pos := range m.PartsOfSpeech {
		for _, meaning := range pos.Meanings {
			if containsFold(meaning, query) {
				return true
			}
		}
	}
	return false
}

func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// DeleteAll clears the whole vocabulary table (clear business data).
func (r *VocabularyRepo) DeleteAll(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM vocabulary`); err != nil {
		return fmt.Errorf("vocabulary delete all: %w", err)
	}
	return nil
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
