package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/amin-jalali/termocode/internal/clock"
)

// Seen is one item a workflow handled before.
type Seen struct {
	Run    string `json:"run"`
	AtMs   int64  `json:"at_ms"`
	Result string `json:"result,omitempty"`
}

// SeenSet is <root>/<workflow>.seen.json: the items a workflow already
// handled, so a filter can skip them (`seen(key)`) on the next run.
type SeenSet struct {
	mu   sync.Mutex
	path string
	m    map[string]Seen
}

// OpenSeen loads the seen items of workflow.
func OpenSeen(root, workflow string) (*SeenSet, error) {
	if err := checkName(workflow); err != nil {
		return nil, err
	}
	s := &SeenSet{path: filepath.Join(root, workflow+".seen.json"), m: map[string]Seen{}}
	b, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(b, &s.m); err != nil {
		return nil, fmt.Errorf("seen items %s: %w", s.path, err)
	}
	if s.m == nil {
		s.m = map[string]Seen{}
	}
	return s, nil
}

// Has says whether key was handled before.
func (s *SeenSet) Has(key string) bool {
	_, ok := s.Get(key)
	return ok
}

// Get is the record of key.
func (s *SeenSet) Get(key string) (Seen, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[redact(key)]
	return v, ok
}

// Mark records key as handled by run with result, and saves.
func (s *SeenSet) Mark(key, run, result string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[redact(key)] = Seen{Run: run, AtMs: clock.Now().UnixMilli(), Result: redact(result)}
	return s.saveLocked()
}

// Forget drops key (`termocode workflow run --again <key>`).
func (s *SeenSet) Forget(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, redact(key))
	return s.saveLocked()
}

func (s *SeenSet) saveLocked() error {
	b, err := json.MarshalIndent(s.m, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.path, b)
}
