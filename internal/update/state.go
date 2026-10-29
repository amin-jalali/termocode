package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// CheckEvery is the least time between two background checks.
const CheckEvery = 24 * time.Hour

// State is what termocode remembers between runs
// ($XDG_STATE_HOME/termocode/update.json).
type State struct {
	LastCheck time.Time `json:"last_check"`
	Latest    string    `json:"latest,omitempty"`    // newest release tag seen
	NotesURL  string    `json:"notes_url,omitempty"` // its release page
	Skipped   string    `json:"skipped,omitempty"`   // "Skip this version"
	Notified  string    `json:"notified,omitempty"`  // the toast was shown for this tag
}

// StatePath is the state file; "" when there is no home folder.
func StatePath(getenv func(string) string) string {
	dir := getenv("XDG_STATE_HOME")
	if dir == "" {
		home := getenv("HOME")
		if home == "" {
			return ""
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "termocode", "update.json")
}

// LoadState reads the state; a missing or broken file is an empty state.
func LoadState(path string) State {
	var s State
	if path == "" {
		return s
	}
	if b, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(b, &s) != nil {
			s = State{}
		}
	}
	return s
}

// SaveState writes the state (temp file + rename).
func SaveState(path string, s State) error {
	if path == "" {
		return os.ErrNotExist
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".update-*.json")
	if err != nil {
		return err
	}
	_, err = tmp.Write(append(b, '\n'))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// Due reports whether a background check may run now: CheckEvery after
// the last one (or when the clock went back).
func Due(s State, now time.Time) bool {
	return s.LastCheck.IsZero() || now.Sub(s.LastCheck) >= CheckEvery || now.Before(s.LastCheck)
}

// Available is the newer release the chip shows for the running version
// current, "" when there is none or the user skipped it.
func (s State) Available(current string) string {
	if s.Latest == "" || s.Latest == s.Skipped || !Newer(current, s.Latest) {
		return ""
	}
	return s.Latest
}

// ShouldToast reports whether the one toast for the available version is
// still due.
func (s State) ShouldToast(current string) bool {
	v := s.Available(current)
	return v != "" && v != s.Notified
}
