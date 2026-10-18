// Package secretstest gives tests a secret store without files to set
// up: an in-memory backend (optionally passphrase-locked, like age) and
// a Store over it with its index in a temp dir.
package secretstest

import (
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/secrets"
)

// Memory is an in-memory secrets.Backend. With a passphrase it starts
// locked and Unlock checks it, like the age file; its values survive
// Lock (they are the "file"), only access is blocked.
type Memory struct {
	mu     sync.Mutex
	pass   string
	locked bool
	data   map[string]string
}

// NewMemory is an unlocked backend with no passphrase.
func NewMemory() *Memory { return &Memory{data: map[string]string{}} }

// NewLockedMemory is a backend that starts locked behind pass.
func NewLockedMemory(pass string) *Memory {
	return &Memory{pass: pass, locked: true, data: map[string]string{}}
}

func (m *Memory) Name() string { return "memory" }

func (m *Memory) Locked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.locked
}

func (m *Memory) Unlock(pass string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pass != "" && pass != m.pass {
		return secrets.ErrWrongPassphrase
	}
	m.locked = false
	return nil
}

func (m *Memory) Lock() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.locked = m.pass != ""
}

func (m *Memory) Get(name string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locked {
		return "", false, secrets.ErrLocked
	}
	v, ok := m.data[name]
	return v, ok, nil
}

func (m *Memory) Set(name, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locked {
		return secrets.ErrLocked
	}
	m.data[name] = value
	return nil
}

func (m *Memory) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locked {
		return secrets.ErrLocked
	}
	delete(m.data, name)
	return nil
}

// Values is a copy of what the backend holds, for assertions.
func (m *Memory) Values() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]string, len(m.data))
	for k, v := range m.data {
		out[k] = v
	}
	return out
}

// New is a Store over b (nil: NewMemory()) with its index in a temp dir
// and an empty environment for ${env:} references.
func New(t testing.TB, b secrets.Backend) secrets.Store {
	t.Helper()
	return NewWithOptions(t, b, secrets.Options{})
}

// NewWithOptions is New with options; Dir defaults to a temp dir and
// Getenv to an empty environment.
func NewWithOptions(t testing.TB, b secrets.Backend, o secrets.Options) secrets.Store {
	t.Helper()
	if b == nil {
		b = NewMemory()
	}
	if o.Dir == "" {
		o.Dir = t.TempDir()
	}
	if o.Getenv == nil {
		o.Getenv = func(string) string { return "" }
	}
	return secrets.NewStore(b, o)
}
