package repository

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"
)

// HistoryRepo provides access to translation_history (docs/05 §2).
type HistoryRepo struct {
	db *sql.DB
}

// NewHistoryRepo builds a HistoryRepo on db.
func NewHistoryRepo(db *sql.DB) *HistoryRepo { return &HistoryRepo{db: db} }

// HistoryRow is one translation history entry.
type HistoryRow struct {
	ID             string
	Kind           string
	InputText      string
	NormalizedText string // column normalized_input
	ResultJSON     string
	Source         string // model | cache
	Model          string
	CreatedAt      string
	LastViewedAt   string
}

// Insert stores a new history row.
func (r *HistoryRepo) Insert(ctx context.Context, row HistoryRow) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO translation_history(id, kind, input_text, normalized_input, result_json, source, model, created_at, last_viewed_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ID, row.Kind, row.InputText, row.NormalizedText, row.ResultJSON,
		row.Source, row.Model, row.CreatedAt, row.LastViewedAt,
	)
	if err != nil {
		return fmt.Errorf("history insert: %w", err)
	}
	return nil
}

// Get returns the history row with id, or ErrNotFound.
func (r *HistoryRepo) Get(ctx context.Context, id string) (HistoryRow, error) {
	var row HistoryRow
	err := r.db.QueryRowContext(ctx,
		`SELECT id, kind, input_text, normalized_input, result_json, source, model, created_at, last_viewed_at
		 FROM translation_history WHERE id = ?`, id,
	).Scan(&row.ID, &row.Kind, &row.InputText, &row.NormalizedText, &row.ResultJSON,
		&row.Source, &row.Model, &row.CreatedAt, &row.LastViewedAt)
	if err == sql.ErrNoRows {
		return HistoryRow{}, ErrNotFound
	}
	if err != nil {
		return HistoryRow{}, fmt.Errorf("history get %q: %w", id, err)
	}
	return row, nil
}

// UpdateResult replaces the result of an existing history row (used by the
// retranslate flow, which must update the current history entry).
func (r *HistoryRepo) UpdateResult(ctx context.Context, id string, row HistoryRow) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE translation_history
		 SET result_json = ?, source = ?, model = ?, created_at = ?, last_viewed_at = ?
		 WHERE id = ?`,
		row.ResultJSON, row.Source, row.Model, row.CreatedAt, row.LastViewedAt, id,
	)
	if err != nil {
		return fmt.Errorf("history update %q: %w", id, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListParams filters and paginates history listing.
type ListParams struct {
	Query  string // substring match on input_text
	Kind   string // "" | word | text
	Limit  int    // 1..200, default 50
	Cursor string // opaque cursor from the previous page
}

// cursor is the payload encoded into the opaque cursor string (Phase 2
// freeze): base64url-encoding of `<created_at RFC3339>|<id>` of the LAST
// item on the previous page. The cursor is opaque to clients.
type cursor struct {
	CreatedAt string
	ID        string
}

func encodeCursor(c cursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.CreatedAt + "|" + c.ID))
}

func decodeCursor(s string) (cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		// Tolerate padded base64url input.
		var err2 error
		raw, err2 = base64.URLEncoding.DecodeString(s)
		if err2 != nil {
			return cursor{}, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
		}
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return cursor{}, fmt.Errorf("%w: malformed cursor payload", ErrInvalidCursor)
	}
	return cursor{CreatedAt: parts[0], ID: parts[1]}, nil
}

const defaultHistoryLimit = 50
const maxHistoryLimit = 200

// List returns history rows newest-first (created_at DESC, id DESC) plus the
// opaque cursor of the next page ("", when there are no more rows).
func (r *HistoryRepo) List(ctx context.Context, p ListParams) ([]HistoryRow, string, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	if limit > maxHistoryLimit {
		limit = maxHistoryLimit
	}

	where := []string{"1=1"}
	args := []any{}
	if p.Kind != "" {
		where = append(where, `kind = ?`)
		args = append(args, p.Kind)
	}
	if p.Query != "" {
		where = append(where, `input_text LIKE ? ESCAPE '\'`)
		args = append(args, "%"+likeEscape(p.Query)+"%")
	}
	if p.Cursor != "" {
		c, err := decodeCursor(p.Cursor)
		if err != nil {
			return nil, "", err
		}
		where = append(where, `(created_at < ? OR (created_at = ? AND id < ?))`)
		args = append(args, c.CreatedAt, c.CreatedAt, c.ID)
	}

	q := `SELECT id, kind, input_text, normalized_input, result_json, source, model, created_at, last_viewed_at
	      FROM translation_history WHERE ` + strings.Join(where, " AND ") + `
	      ORDER BY created_at DESC, id DESC
	      LIMIT ?`
	args = append(args, limit+1)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, "", fmt.Errorf("history list: %w", err)
	}
	defer rows.Close()

	var out []HistoryRow
	for rows.Next() {
		var row HistoryRow
		if err := rows.Scan(&row.ID, &row.Kind, &row.InputText, &row.NormalizedText, &row.ResultJSON,
			&row.Source, &row.Model, &row.CreatedAt, &row.LastViewedAt); err != nil {
			return nil, "", fmt.Errorf("history list scan: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("history list: %w", err)
	}

	next := ""
	if len(out) > limit {
		last := out[limit-1]
		out = out[:limit]
		next = encodeCursor(cursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return out, next, nil
}

// Delete removes a single history row. Cache rows are intentionally kept.
// Returns ErrNotFound when the id does not exist.
func (r *HistoryRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM translation_history WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("history delete %q: %w", id, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteAll clears the whole history table (docs/04 §6 clearHistory).
func (r *HistoryRepo) DeleteAll(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM translation_history`); err != nil {
		return fmt.Errorf("history delete all: %w", err)
	}
	return nil
}

// likeEscape escapes LIKE wildcards in user-provided query text.
func likeEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
