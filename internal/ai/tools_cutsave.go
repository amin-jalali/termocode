package ai

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// ── The full output of a cut command ──────────────────────────────────────
//
// run_command keeps about 16 KB of an output (capHeadTail). When it cuts,
// the whole output is saved in a termocode folder outside the repo
// ($XDG_CACHE_HOME/termocode/ai-output/<chat>/run-N.log, 0600) and the cut
// note names the file, so the model can read the dropped middle with
// read_file and an offset. read_file may read that folder, nothing else
// outside the workspace; no tool writes there. Secrets are hidden before
// the file is written, as in a tool result.

const (
	// cutSaveMax caps one saved output; a bigger one is itself cut in
	// the middle.
	cutSaveMax = 5 << 20
	// cutSaveKeep is how long saved outputs stay: older chat folders go
	// at the first save of a new one.
	cutSaveKeep = 7 * 24 * time.Hour
)

// cutOutputRoot is the folder of every chat's saved outputs.
func cutOutputRoot() (string, error) {
	c := os.Getenv("XDG_CACHE_HOME")
	if c == "" {
		var err error
		if c, err = os.UserCacheDir(); err != nil {
			return "", err
		}
	}
	return filepath.Join(c, "termocode", "ai-output"), nil
}

// outStore is one chat's (one tool set's) saved outputs; its folder is
// made at the first save.
type outStore struct {
	once sync.Once
	dir  string
	err  error
	n    atomic.Int64
}

func (s *outStore) open() {
	root, err := cutOutputRoot()
	if err == nil {
		err = os.MkdirAll(root, 0o700)
	}
	if err != nil {
		s.err = err
		return
	}
	pruneCutOutputs(root, time.Now().Add(-cutSaveKeep))
	s.dir, s.err = os.MkdirTemp(root, "chat-")
}

// save writes full (secrets hidden) and gives its path.
func (s *outStore) save(full string) (string, error) {
	s.once.Do(s.open)
	if s.err != nil {
		return "", s.err
	}
	if len(full) > cutSaveMax {
		full = capHeadTail(full, cutSaveMax)
	}
	p := filepath.Join(s.dir, fmt.Sprintf("run-%d.log", s.n.Add(1)))
	if err := os.WriteFile(p, []byte(RedactSecrets(full)), 0o600); err != nil {
		return "", err
	}
	return p, nil
}

// pruneCutOutputs removes the saved outputs not changed since before.
func pruneCutOutputs(root string, before time.Time) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range ents {
		if fi, err := e.Info(); err == nil && fi.ModTime().Before(before) {
			_ = os.RemoveAll(filepath.Join(root, e.Name())) // best effort: a file left is cleaned next time
		}
	}
}

// capRun is capHeadTail for a command's output: a cut output is saved in
// full and the cut note says where (a failed save keeps the plain note).
func (w Workspace) capRun(s string) string {
	if len(s) <= maxOutBytes || w.out == nil {
		return capHeadTail(s, maxOutBytes)
	}
	p, err := w.out.save(s)
	if err != nil {
		return capHeadTail(s, maxOutBytes)
	}
	return capHeadTailNote(s, maxOutBytes, p)
}

// savedOutputFile is the real path of p when it is a saved output: an
// absolute path whose real place (links followed) is a regular file in
// the saved-output folder.
func savedOutputFile(p string) (string, bool) {
	if !filepath.IsAbs(p) {
		return "", false
	}
	root, err := cutOutputRoot()
	if err != nil {
		return "", false
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil || real == realRoot || !within(realRoot, real) {
		return "", false
	}
	if fi, err := os.Stat(real); err != nil || !fi.Mode().IsRegular() {
		return "", false
	}
	return real, true
}

// readOutsideOrSaved is read_file's answer for a path Resolve refused:
// a saved output is read (from this machine, also for a container
// workspace), anything else keeps the error.
func readOutsideOrSaved(p string, a map[string]any, rerr error) (string, error) {
	if !errors.Is(rerr, ErrOutsideWorkspace) {
		return "", rerr
	}
	real, ok := savedOutputFile(p)
	if !ok {
		return "", rerr
	}
	b, err := os.ReadFile(real)
	if err != nil {
		return "", err
	}
	return numberedRead(real, RedactSecrets(string(b)), a)
}

// withOutStore gives w a store for its cut outputs when it has none.
func withOutStore(w Workspace) Workspace {
	if w.out == nil {
		w.out = &outStore{}
	}
	return w
}
