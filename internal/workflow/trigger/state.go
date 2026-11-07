package trigger

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// MaxSeen caps the remembered keys of one trigger (oldest go first).
const MaxSeen = 2000

// State is what the pollers remember between polls and restarts:
// per trigger, the keys already seen and the labels of each issue. It
// lives outside the repo (<workflow state root>/triggers.json), 0600.
type State struct {
	path string
	mu   sync.Mutex
	data map[string]*trigState
}

type trigState struct {
	// Ready: the first poll is done (it only records, it never fires).
	Ready  bool                `json:"ready"`
	Seen   map[string]int64    `json:"seen,omitempty"`   // key → order seen
	Labels map[string][]string `json:"labels,omitempty"` // issue number → labels
	N      int64               `json:"n"`
}

// OpenState reads the state file in dir (a missing file is empty).
func OpenState(dir string) (*State, error) {
	s := &State{path: filepath.Join(dir, "triggers.json"), data: map[string]*trigState{}}
	b, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		s.data = map[string]*trigState{} // a broken file starts over (it only re-baselines)
	}
	return s, nil
}

// MemState is a State that is never saved (tests, a missing root).
func MemState() *State { return &State{data: map[string]*trigState{}} }

func (s *State) get(id string) *trigState {
	t := s.data[id]
	if t == nil {
		t = &trigState{Seen: map[string]int64{}, Labels: map[string][]string{}}
		s.data[id] = t
	}
	if t.Seen == nil {
		t.Seen = map[string]int64{}
	}
	if t.Labels == nil {
		t.Labels = map[string][]string{}
	}
	return t
}

func (t *trigState) mark(key string) bool {
	if _, ok := t.Seen[key]; ok {
		return false
	}
	t.N++
	t.Seen[key] = t.N
	if len(t.Seen) > MaxSeen {
		keys := make([]string, 0, len(t.Seen))
		for k := range t.Seen {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return t.Seen[keys[i]] < t.Seen[keys[j]] })
		for _, k := range keys[:len(keys)-MaxSeen] {
			delete(t.Seen, k)
		}
	}
	return true
}

// Keep drops the state of every trigger id not in ids (a removed or
// changed trigger).
func (s *State) Keep(ids map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.data {
		if !ids[id] {
			delete(s.data, id)
		}
	}
}

// save writes the file atomically; the caller holds mu.
func (s *State) save() error {
	if s.path == "" {
		return nil
	}
	b, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".triggers.json.tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, s.path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
