// Package ulcid generates sortable unique identifiers for domain entities:
// 26-character ULID strings (docs/04 §1: ULID chosen for time-ordered IDs).
package ulcid

import (
	"crypto/rand"
	"sync"

	"github.com/oklog/ulid/v2"
)

var (
	mu        sync.Mutex
	monotonic = ulid.Monotonic(rand.Reader, 0)
)

// New returns a fresh 26-character uppercase ULID string. Generation is
// monotonic (safe for concurrent callers through a mutex) so IDs created in
// the same millisecond still sort in creation order. On the (astronomically
// unlikely) monotonic overflow the function falls back to a fresh random
// ULID, which remains a valid 26-character identifier.
func New() string {
	mu.Lock()
	defer mu.Unlock()
	id, err := ulid.New(ulid.Now(), monotonic)
	if err != nil {
		return ulid.Make().String()
	}
	return id.String()
}
