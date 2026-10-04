package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// CacheRepo provides access to translation_cache (docs/05 §2).
type CacheRepo struct {
	db *sql.DB
}

// NewCacheRepo builds a CacheRepo on db.
func NewCacheRepo(db *sql.DB) *CacheRepo { return &CacheRepo{db: db} }

// CacheRow is one cached translation result.
type CacheRow struct {
	CacheKey        string
	Kind            string
	NormalizedInput string
	Model           string
	ConfigHash      string
	ResultJSON      string
	CreatedAt       string
	UpdatedAt       string
	LastUsedAt      string
}

// Get returns the cache row for cacheKey, or ErrNotFound.
func (r *CacheRepo) Get(ctx context.Context, cacheKey string) (CacheRow, error) {
	var row CacheRow
	err := r.db.QueryRowContext(ctx,
		`SELECT cache_key, kind, normalized_input, model, config_hash, result_json, created_at, updated_at, last_used_at
		 FROM translation_cache WHERE cache_key = ?`, cacheKey,
	).Scan(&row.CacheKey, &row.Kind, &row.NormalizedInput, &row.Model, &row.ConfigHash,
		&row.ResultJSON, &row.CreatedAt, &row.UpdatedAt, &row.LastUsedAt)
	if err == sql.ErrNoRows {
		return CacheRow{}, ErrNotFound
	}
	if err != nil {
		return CacheRow{}, fmt.Errorf("cache get: %w", err)
	}
	return row, nil
}

// Upsert inserts or replaces a cache row (bypass_cache overwrites on success).
func (r *CacheRepo) Upsert(ctx context.Context, row CacheRow) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO translation_cache(cache_key, kind, normalized_input, model, config_hash, result_json, created_at, updated_at, last_used_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(cache_key) DO UPDATE SET
		   kind = excluded.kind,
		   normalized_input = excluded.normalized_input,
		   model = excluded.model,
		   config_hash = excluded.config_hash,
		   result_json = excluded.result_json,
		   updated_at = excluded.updated_at,
		   last_used_at = excluded.last_used_at`,
		row.CacheKey, row.Kind, row.NormalizedInput, row.Model, row.ConfigHash,
		row.ResultJSON, row.CreatedAt, row.UpdatedAt, row.LastUsedAt,
	)
	if err != nil {
		return fmt.Errorf("cache upsert: %w", err)
	}
	return nil
}

// Touch updates last_used_at after a cache hit.
func (r *CacheRepo) Touch(ctx context.Context, cacheKey, lastUsedAt string) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE translation_cache SET last_used_at = ? WHERE cache_key = ?`,
		lastUsedAt, cacheKey,
	); err != nil {
		return fmt.Errorf("cache touch: %w", err)
	}
	return nil
}
