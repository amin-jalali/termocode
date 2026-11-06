package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// WorkflowState is <root>/<name>.state.json (plan §3.7). Allow and
// TrustedHash hold only for the file bytes they were given for: a load
// with another hash drops them, so a repo cannot pre-approve its own
// writes and a changed file asks again.
type WorkflowState struct {
	Enabled     bool     `json:"enabled"`
	Allow       []string `json:"allow,omitempty"`
	AllowHash   string   `json:"allow_hash,omitempty"`
	TrustedHash string   `json:"trusted_hash,omitempty"`
	LastRunMs   int64    `json:"last_run_ms,omitempty"`
	NextRunMs   int64    `json:"next_run_ms,omitempty"`
}

// stateMu serialises the read-modify-write of state files in this
// process.
var stateMu sync.Mutex

func statePath(root, name string) string { return filepath.Join(root, name+".state.json") }

// LoadState reads the state of workflow name for the file with hash; an
// allow-list or trust of other bytes is dropped (and the drop saved).
func LoadState(root, name, hash string) (WorkflowState, error) {
	stateMu.Lock()
	defer stateMu.Unlock()
	return loadStateLocked(root, name, hash)
}

func loadStateLocked(root, name, hash string) (WorkflowState, error) {
	var st WorkflowState
	if err := checkName(name); err != nil {
		return st, err
	}
	b, err := os.ReadFile(statePath(root, name))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return st, nil
	case err != nil:
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return WorkflowState{}, fmt.Errorf("workflow state %s: %w", statePath(root, name), err)
	}
	changed := false
	if (len(st.Allow) > 0 || st.AllowHash != "") && (hash == "" || st.AllowHash != hash) {
		st.Allow, st.AllowHash, changed = nil, "", true
	}
	if st.TrustedHash != "" && st.TrustedHash != hash {
		st.TrustedHash, changed = "", true
	}
	if changed {
		return st, saveStateLocked(root, name, st)
	}
	return st, nil
}

// SaveState writes the state of workflow name.
func SaveState(root, name string, st WorkflowState) error {
	stateMu.Lock()
	defer stateMu.Unlock()
	return saveStateLocked(root, name, st)
}

func saveStateLocked(root, name string, st WorkflowState) error {
	if err := checkName(name); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(statePath(root, name), b)
}

// Allowed says whether step of workflow name may write without the card
// for the file with hash.
func Allowed(root, name, hash, step string) (bool, error) {
	if hash == "" {
		return false, nil
	}
	st, err := LoadState(root, name, hash)
	if err != nil {
		return false, err
	}
	return st.AllowHash == hash && slices.Contains(st.Allow, step), nil
}

// AllowStep is the "Allow for this workflow" answer: step joins the
// allow-list, bound to hash.
func AllowStep(root, name, hash, step string) error {
	if hash == "" || step == "" {
		return errors.New("workflow state: allow needs the file hash and the step")
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	st, err := loadStateLocked(root, name, hash)
	if err != nil {
		return err
	}
	st.AllowHash = hash
	if !slices.Contains(st.Allow, step) {
		st.Allow = append(st.Allow, step)
		slices.Sort(st.Allow)
	}
	return saveStateLocked(root, name, st)
}
