package now

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// keepDone is how long a done item is remembered.
const keepDone = 30 * 24 * time.Hour

// maxUndo caps the undo steps kept in memory.
const maxUndo = 20

// State is the user's Now choices, kept in the config folder (per user,
// not per project). Times are Unix ms.
type State struct {
	Setup   bool             `json:"setup"`
	Answers Answers          `json:"answers"`
	Rules   []Rule           `json:"rules"`
	Done    map[string]int64 `json:"done,omitempty"`
	Later   map[string]int64 `json:"later,omitempty"`
	Muted   map[string]int64 `json:"muted,omitempty"`
}

func defaultState() State {
	return State{Answers: DefaultAnswers(), Rules: DefaultRules(), Done: map[string]int64{}, Later: map[string]int64{}, Muted: map[string]int64{}}
}

// undoStep puts one map entry back as it was.
type undoStep struct {
	what string // "done", "later", "muted"
	key  string
	old  int64
	had  bool
}

// Store keeps State on disk. A Store with no path keeps it in memory.
type Store struct {
	mu   sync.Mutex
	path string
	st   State
	undo []undoStep
}

// Open reads the store at path; a missing or broken file reads as the
// defaults.
func Open(path string) *Store {
	s := &Store{path: path, st: defaultState()}
	if path == "" {
		return s
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var st State
	if json.Unmarshal(b, &st) != nil {
		return s
	}
	if len(st.Rules) == 0 || len(st.Rules) > MaxRules {
		st.Rules = DefaultRules()
	}
	if st.Answers.Show == 0 && len(st.Answers.Top) == 0 && len(st.Answers.From) == 0 {
		st.Answers = DefaultAnswers()
	}
	for _, m := range []*map[string]int64{&st.Done, &st.Later, &st.Muted} {
		if *m == nil {
			*m = map[string]int64{}
		}
	}
	s.st = st
	return s
}

// State is a copy of the store's state.
func (s *Store) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.st
	st.Answers.Top = append([]string(nil), st.Answers.Top...)
	st.Answers.From = append([]string(nil), st.Answers.From...)
	st.Rules = append([]Rule(nil), st.Rules...)
	st.Done, st.Later, st.Muted = cloneMap(st.Done), cloneMap(st.Later), cloneMap(st.Muted)
	return st
}

// Prefs are the store's rules and sources for Place.
func (s *Store) Prefs() Prefs {
	st := s.State()
	return Prefs{Rules: st.Rules, From: st.Answers.From}
}

// Setup keeps the first-run answers (a skip keeps the defaults) and makes
// the rules from them.
func (s *Store) Setup(a Answers) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a.Show = a.ShowN()
	s.st.Setup, s.st.Answers, s.st.Rules = true, a, RulesFor(a)
	return s.saveLocked()
}

// SetRules replaces the rules (at most MaxRules, each valid, ids unique).
func (s *Store) SetRules(rules []Rule) error {
	if len(rules) > MaxRules {
		return fmt.Errorf("at most %d rules", MaxRules)
	}
	seen := map[string]bool{}
	for _, r := range rules {
		if err := ValidRule(r); err != nil {
			return err
		}
		if seen[r.ID] {
			return fmt.Errorf("rule %q is there twice", r.ID)
		}
		seen[r.ID] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Rules = append([]Rule(nil), rules...)
	s.st.Setup = true
	return s.saveLocked()
}

// Done marks id done at at.
func (s *Store) Done(id string, at time.Time) error { return s.set("done", id, at.UnixMilli()) }

// Later hides id until until.
func (s *Store) Later(id string, until time.Time) error { return s.set("later", id, until.UnixMilli()) }

// Mute hides key's items until someone asks me (see MuteKey).
func (s *Store) Mute(key string, at time.Time) error { return s.set("muted", key, at.UnixMilli()) }

// MuteKey is the mute key of it: the item itself, or its repo.
func MuteKey(it Item, repo bool) string {
	if repo && it.Repo != "" {
		return "repo:" + it.Repo
	}
	return "item:" + it.ID
}

func (s *Store) set(what, key string, v int64) error {
	if key == "" {
		return fmt.Errorf("no item")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.mapLocked(what)
	old, had := m[key]
	m[key] = v
	s.undo = append(s.undo, undoStep{what: what, key: key, old: old, had: had})
	if len(s.undo) > maxUndo {
		s.undo = s.undo[len(s.undo)-maxUndo:]
	}
	return s.saveLocked()
}

func (s *Store) mapLocked(what string) map[string]int64 {
	switch what {
	case "done":
		return s.st.Done
	case "later":
		return s.st.Later
	}
	return s.st.Muted
}

// Undo takes the last Done, Later or Mute back and names its key.
func (s *Store) Undo() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.undo) == 0 {
		return "", false
	}
	u := s.undo[len(s.undo)-1]
	s.undo = s.undo[:len(s.undo)-1]
	m := s.mapLocked(u.what)
	if u.had {
		m[u.key] = u.old
	} else {
		delete(m, u.key)
	}
	_ = s.saveLocked()
	return u.key, true
}

// Prune forgets done items older than 30 days and laters long past.
func (s *Store) Prune(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cut := now.Add(-keepDone).UnixMilli()
	changed := false
	for k, v := range s.st.Done {
		if v < cut {
			delete(s.st.Done, k)
			changed = true
		}
	}
	for k, v := range s.st.Later {
		if v < cut {
			delete(s.st.Later, k)
			changed = true
		}
	}
	if !changed {
		return
	}
	kept := s.undo[:0]
	for _, u := range s.undo {
		if _, ok := s.mapLocked(u.what)[u.key]; ok {
			kept = append(kept, u)
		}
	}
	s.undo = kept
	_ = s.saveLocked()
}

// asksMe: an item that asks me (a review, a mention) breaks a mute.
func asksMe(it Item) bool { return it.Signal == SigReview || it.Signal == SigMention }

// Hidden reports whether the user put it away: done (and nothing new
// since), later (not yet), or muted (and it does not ask me).
func (s *Store) Hidden(it Item, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hiddenLocked(it, now)
}

func (s *Store) hiddenLocked(it Item, now time.Time) bool {
	if d, ok := s.st.Done[it.ID]; ok {
		when := it.WhyAt
		if when.IsZero() {
			when = it.Since
		}
		if when.UnixMilli() <= d {
			return true
		}
	}
	if l, ok := s.st.Later[it.ID]; ok && now.UnixMilli() < l {
		return true
	}
	if !asksMe(it) {
		if _, ok := s.st.Muted[MuteKey(it, false)]; ok {
			return true
		}
		if _, ok := s.st.Muted[MuteKey(it, true)]; ok && it.Repo != "" {
			return true
		}
	}
	return false
}

// Apply drops the hidden items and puts the ones back from Later on
// your turn, with WhyBack as their reason.
func (s *Store) Apply(items []Item, now time.Time) []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if s.hiddenLocked(it, now) {
			continue
		}
		if l, ok := s.st.Later[it.ID]; ok && now.UnixMilli() >= l {
			it.Turn, it.Why, it.WhyAt = TurnMe, WhyBack, time.UnixMilli(l)
		}
		out = append(out, it)
	}
	return out
}

// saveLocked writes the state (0600) through a temp file.
func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".now-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

func cloneMap(m map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
