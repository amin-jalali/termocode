// Package runner makes this machine serve agent tasks (CMP-12). A task
// (a folder, a prompt and an agent CLI) waits as one JSON file in the
// queue folder until `termocode runner serve` takes it. The runner runs
// at most Pool tasks at once, each through clistep (the user's own
// Claude Code or Codex login, prompt on stdin, secrets out of the
// environment). A task that may change files gets its own agent
// worktree, so it never touches the folder's checkout; a read-only task
// runs in the folder itself. The runner serves only the folders it was
// started for. Remote clients add tasks with `termocode runner add`
// over ssh; the answer, the changed files and a log stay with the task.
package runner

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Task states.
const (
	StateQueued   = "queued"
	StateRunning  = "running"
	StateDone     = "done"
	StateFailed   = "failed"
	StateCanceled = "canceled"
)

// Limits.
const (
	MaxPrompt = 64 << 10 // bytes of one prompt
	MaxAnswer = 64 << 10 // bytes of the kept answer
	MaxLog    = 4 << 20  // bytes of one task log
)

// Task is one queued agent task.
type Task struct {
	ID     string `json:"id"`
	Folder string `json:"folder"` // absolute; the workspace (or a folder in it)
	Prompt string `json:"prompt"`
	Agent  string `json:"agent"` // ai.CLIClaude or ai.CLICodex
	Model  string `json:"model,omitempty"`
	// ReadOnly runs the agent in Folder without file changes; else it
	// gets its own worktree from Base ("" the folder's HEAD).
	ReadOnly bool   `json:"read_only,omitempty"`
	Base     string `json:"base,omitempty"`

	State       string    `json:"state"`
	CancelAsked bool      `json:"cancel_asked,omitempty"`
	Created     time.Time `json:"created"`
	Started     time.Time `json:"started,omitzero"`
	Ended       time.Time `json:"ended,omitzero"`

	Worktree string   `json:"worktree,omitempty"` // the agent worktree folder
	Branch   string   `json:"branch,omitempty"`
	Answer   string   `json:"answer,omitempty"`
	Files    []string `json:"files,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// Final reports a state that never changes again.
func Final(state string) bool {
	return state == StateDone || state == StateFailed || state == StateCanceled
}

// Agents are the CLIs a task may name.
var Agents = []string{ai.CLIClaude, ai.CLICodex}

func validAgent(a string) bool {
	for _, x := range Agents {
		if a == x {
			return true
		}
	}
	return false
}

// DefaultDir is the queue folder: <config>/runner.
func DefaultDir() (string, error) {
	d, err := ai.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "runner"), nil
}

// Store is the queue folder: one <id>.json per task, <id>.log beside it.
type Store struct {
	Dir string
	Now func() time.Time // nil: time.Now
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

var idRe = regexp.MustCompile(`^t[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$`)

// ValidID reports a task id (never a path).
func ValidID(id string) bool { return idRe.MatchString(id) }

func (s *Store) path(id string) string { return filepath.Join(s.Dir, id+".json") }

// LogPath is the log file of a task.
func (s *Store) LogPath(id string) string { return filepath.Join(s.Dir, id+".log") }

func (s *Store) ensure() error { return os.MkdirAll(s.Dir, 0o700) }

// lock holds the queue lock until the returned func.
func (s *Store) lock() (func(), error) {
	if err := s.ensure(); err != nil {
		return nil, err
	}
	return lockFile(filepath.Join(s.Dir, ".lock"))
}

func newID(now time.Time) (string, error) {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "t" + now.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b[:]), nil
}

// check is the shape check of a new task.
func check(t Task) error {
	if !filepath.IsAbs(t.Folder) {
		return fmt.Errorf("folder %q is not an absolute path", t.Folder)
	}
	if fi, err := os.Stat(t.Folder); err != nil || !fi.IsDir() {
		return fmt.Errorf("folder %q is not there", t.Folder)
	}
	if strings.TrimSpace(t.Prompt) == "" {
		return errors.New("the prompt is empty")
	}
	if len(t.Prompt) > MaxPrompt {
		return fmt.Errorf("the prompt is longer than %d KB", MaxPrompt>>10)
	}
	if !validAgent(t.Agent) {
		return fmt.Errorf("agent must be one of %s", strings.Join(Agents, ", "))
	}
	if strings.HasPrefix(t.Model, "-") || strings.ContainsAny(t.Model, " \t\n") {
		return fmt.Errorf("model %q is not a model id", t.Model)
	}
	if strings.HasPrefix(t.Base, "-") || strings.ContainsAny(t.Base, " \t\n") {
		return fmt.Errorf("base %q is not a branch", t.Base)
	}
	return nil
}

// Add queues a new task and returns it with its id.
func (s *Store) Add(t Task) (Task, error) {
	t.Folder = filepath.Clean(t.Folder)
	t.Prompt = strings.TrimSpace(t.Prompt)
	if err := check(t); err != nil {
		return Task{}, err
	}
	unlock, err := s.lock()
	if err != nil {
		return Task{}, err
	}
	defer unlock()
	now := s.now()
	for range 5 {
		id, err := newID(now)
		if err != nil {
			return Task{}, err
		}
		if _, err := os.Stat(s.path(id)); err == nil {
			continue
		}
		t.ID, t.State, t.Created = id, StateQueued, now
		t.CancelAsked, t.Started, t.Ended = false, time.Time{}, time.Time{}
		t.Worktree, t.Branch, t.Answer, t.Files, t.Error = "", "", "", nil, ""
		return t, s.write(t)
	}
	return Task{}, errors.New("no free task id")
}

func (s *Store) write(t Task) error {
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Dir, ".task-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(append(b, '\n'))
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.path(t.ID)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Store) read(id string) (Task, error) {
	if !ValidID(id) {
		return Task{}, fmt.Errorf("%q is not a task id", id)
	}
	b, err := os.ReadFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return Task{}, fmt.Errorf("no task %s", id)
	}
	if err != nil {
		return Task{}, err
	}
	var t Task
	if err := json.Unmarshal(b, &t); err != nil {
		return Task{}, fmt.Errorf("task %s: %w", id, err)
	}
	if t.ID != id {
		return Task{}, fmt.Errorf("task %s: the file names another id", id)
	}
	return t, nil
}

// Get reads one task.
func (s *Store) Get(id string) (Task, error) { return s.read(id) }

// List is every task, oldest first. A broken file is left out.
func (s *Store) List() ([]Task, error) {
	ents, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Task
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !e.Type().IsRegular() || !ValidID(id) {
			continue
		}
		t, err := s.read(id)
		if err != nil {
			continue
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.Before(out[j].Created)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Update changes one task under the queue lock.
func (s *Store) Update(id string, f func(*Task) error) (Task, error) {
	unlock, err := s.lock()
	if err != nil {
		return Task{}, err
	}
	defer unlock()
	t, err := s.read(id)
	if err != nil {
		return Task{}, err
	}
	if err := f(&t); err != nil {
		return t, err
	}
	t.ID = id
	return t, s.write(t)
}

// Cancel ends a queued task at once; a running one is asked to stop
// (the runner stops its agent on its next look).
func (s *Store) Cancel(id string) (Task, error) {
	return s.Update(id, func(t *Task) error {
		switch t.State {
		case StateQueued:
			t.State, t.Ended = StateCanceled, s.now()
		case StateRunning:
			t.CancelAsked = true
		default:
			return fmt.Errorf("task %s is already %s", id, t.State)
		}
		return nil
	})
}
