package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// SessionState persists the editor's open-buffer list across launches so
// re-opening termocode doesn't reset the workspace to the welcome screen.
// Cursors maps a file path to its last-known [line, col] (1-based) so a
// relaunch can drop the cursor back where the user left it in each buffer.
// Scroll state is intentionally not stored — `:call cursor()` plus nvim's
// 'scrolloff' get the visible region close enough without extra plumbing.
type SessionState struct {
	OpenFiles    []string          `json:"open_files"`
	ActivePath   string            `json:"active_path"`
	ExpandedDirs []string          `json:"expanded_dirs"`
	Cursors      map[string][2]int `json:"cursors"` // path → [line, col] (1-based)
	// Roots are *additional* workspace roots ("Add Folder to Workspace…").
	// The primary cwd-based root isn't stored here — it's whatever cwd the
	// user launched termocode in, which may legitimately differ run-to-run.
	Roots []string `json:"roots,omitempty"`
	// TerminalRows is the user's preferred height for the integrated
	// terminal panel (drag-splitter mutates the live Model field; we persist
	// here so the height survives a relaunch). Zero means "never set" → use
	// the default on first open.
	TerminalRows int `json:"terminal_rows,omitempty"`

	// Right-side Actions panel state. Pointers for the booleans so an
	// unset field (nil) is distinguishable from explicitly-false — on first
	// run we want the panel open AND pinned, but a user who has clicked
	// "close" needs that decision to survive a relaunch as `false`. Width
	// uses an int with a sentinel of 0 = "never set" → fall back to the
	// default on first open.
	ActionsOpen   *bool `json:"actions_open,omitempty"`
	ActionsPinned *bool `json:"actions_pinned,omitempty"`
	ActionsWidth  int   `json:"actions_width,omitempty"`

	// GitViewTree is the Source-Control file list layout preference: tree
	// (true) vs flat list (false). Pointer so an unset field (nil) falls back
	// to the tree default while an explicit choice survives a relaunch.
	GitViewTree *bool `json:"git_view_tree,omitempty"`
}

func sessionPath() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "termocode", "session.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "termocode", "session.json"), nil
}

// loadSession reads the persisted session file. Missing/corrupt → empty.
//
// Defensive filter: drops `term://...` and `... PREVIEW` paths from the
// open-files / cursors maps. Those are scratch/terminal buffers that an
// older termocode wrote out by accident and should never come back —
// reopening them either fails (the term:// URI is meaningless across
// processes) or clones broken UI state.
func loadSession() SessionState {
	path, err := sessionPath()
	if err != nil {
		return SessionState{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return SessionState{}
	}
	var s SessionState
	if err := json.Unmarshal(data, &s); err != nil {
		return SessionState{}
	}
	cleaned := s.OpenFiles[:0]
	for _, p := range s.OpenFiles {
		if isPersistedZombiePath(p) {
			continue
		}
		cleaned = append(cleaned, p)
	}
	s.OpenFiles = cleaned
	if isPersistedZombiePath(s.ActivePath) {
		s.ActivePath = ""
		if len(s.OpenFiles) > 0 {
			s.ActivePath = s.OpenFiles[0]
		}
	}
	for k := range s.Cursors {
		if isPersistedZombiePath(k) {
			delete(s.Cursors, k)
		}
	}
	return s
}

// isPersistedZombiePath reports whether path is the kind of buffer name
// we never want to reopen on relaunch (terminal URIs, markdown preview
// scratch buffers, find-results scratch buffers — those are session-
// scoped and recreating them with stale state just confuses the user).
func isPersistedZombiePath(path string) bool {
	if path == "" {
		return false
	}
	if len(path) >= 7 && path[:7] == "term://" {
		return true
	}
	if len(path) >= 8 && path[len(path)-8:] == " PREVIEW" {
		return true
	}
	if strings.HasSuffix(path, "/Find Results") || path == "Find Results" {
		return true
	}
	if strings.HasSuffix(path, "termocode-find-results.txt") {
		return true
	}
	return false
}

// saveSession writes the open-file list to disk. Errors are swallowed
// (best-effort).
func saveSession(s SessionState) {
	path, err := sessionPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}
