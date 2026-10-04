//go:build !windows

package credential

import "errors"

// Windows builds use the real Credential Manager; every other platform gets
// a store that refuses to operate (FloatTranslate 1.0 is Windows-only, so
// this only exists to keep cross-compilation and vet honest).
type unsupportedStore struct{}

// NewWindows on non-Windows platforms returns a store whose operations fail
// with ErrUnsupportedPlatform.
func NewWindows() Store { return unsupportedStore{} }

// ErrUnsupportedPlatform is returned by the non-Windows store.
var ErrUnsupportedPlatform = errors.New("credential: Credential Manager requires Windows")

func (unsupportedStore) Load() (string, bool, error) { return "", false, ErrUnsupportedPlatform }
func (unsupportedStore) Save(string) error           { return ErrUnsupportedPlatform }
func (unsupportedStore) Delete() error               { return ErrUnsupportedPlatform }
