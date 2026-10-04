// Package repository holds the SQLite data-access layer (one file per
// aggregate). Repositories return sentinel errors (ErrNotFound, ErrConflict)
// that the API layer maps to standard error codes.
package repository

import "errors"

var (
	// ErrNotFound is returned when a requested row does not exist.
	ErrNotFound = errors.New("repository: not found")
	// ErrConflict is returned when a uniqueness constraint would be violated.
	ErrConflict = errors.New("repository: conflict")
	// ErrInvalidCursor is returned when a pagination cursor cannot be
	// decoded; the API layer maps it to 400 INVALID_REQUEST.
	ErrInvalidCursor = errors.New("repository: invalid cursor")
)
