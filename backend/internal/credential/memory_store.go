package credential

import "sync"

// Memory is an in-memory Store used by tests (and nowhere in production
// wiring). It mirrors the Windows store's semantics: Load reports found for
// a previously saved value, Save replaces, Delete is idempotent.
type Memory struct {
	mu    sync.RWMutex
	value string
	found bool
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory { return &Memory{} }

// Load implements Store.
func (m *Memory) Load() (string, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.value, m.found, nil
}

// Save implements Store.
func (m *Memory) Save(value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.value, m.found = value, true
	return nil
}

// Delete implements Store. Deleting a missing credential is a no-op.
func (m *Memory) Delete() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.value, m.found = "", false
	return nil
}
