package wire

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/workflow/run"
)

// RunFiles stores run files under Root/runs/<id>.json (Root is
// ledger.Root of the workspace). It is the engine's RunFileWriter.
type RunFiles struct{ Root string }

var runIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func (s RunFiles) path(id string) (string, error) {
	if !runIDRe.MatchString(id) {
		return "", fmt.Errorf("run file: bad run id %q", id)
	}
	return filepath.Join(s.Root, "runs", id+".json"), nil
}

// WriteRunFile writes the run file atomically (temp file, then rename).
func (s RunFiles) WriteRunFile(id string, data []byte) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	return writeAtomic(p, data)
}

// Load reads one run file.
func (s RunFiles) Load(id string) (*run.RunFile, error) {
	p, err := s.path(id)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return run.Load(b)
}

// List returns the run files of workflow ("" = all), newest first, at
// most limit (0 = all). Unreadable files are skipped.
func (s RunFiles) List(workflow string, limit int) ([]*run.RunFile, error) {
	ents, err := os.ReadDir(filepath.Join(s.Root, "runs"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*run.RunFile
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		rf, err := s.Load(strings.TrimSuffix(name, ".json"))
		if err != nil || (workflow != "" && rf.Workflow != workflow) {
			continue
		}
		out = append(out, rf)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.After(out[j].StartedAt)
		}
		return out[i].ID > out[j].ID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// writeAtomic writes data next to path and renames it over path (0600).
func writeAtomic(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(0o600); err == nil {
		if _, err = tmp.Write(data); err == nil {
			err = tmp.Sync()
		}
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// NewRunID is a run id from the clock: 20260106-090000-123.
func NewRunID() string { return time.Now().UTC().Format("20060102-150405.000") }

// Prune deletes the runs that ended (an interrupted one: started)
// before cut: the run file and the run's folder (write ledger, logs,
// audit). Running runs stay. It returns how many runs it removed.
func (s RunFiles) Prune(cut time.Time) (int, error) {
	all, err := s.List("", 0)
	if err != nil {
		return 0, err
	}
	n := 0
	var first error
	for _, rf := range all {
		end := rf.EndedAt
		if end.IsZero() { // interrupted: it never ended
			end = rf.StartedAt
		}
		if rf.State == run.RunRunning || end.IsZero() || !end.Before(cut) {
			continue
		}
		p, err := s.path(rf.ID)
		if err != nil {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			if first == nil {
				first = err
			}
			continue
		}
		if err := os.RemoveAll(filepath.Join(s.Root, "runs", rf.ID)); err != nil && first == nil {
			first = err
		}
		n++
	}
	return n, first
}
