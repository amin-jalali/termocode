package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
//
// Storage is split (see loadSession / saveSession): the workspace part
// (open files, cursors, explorer, extra roots, last AI chat) lives in
// sessions/<hash of the workspace path>.json, one file per workspace, so
// switching project never reopens another project's tabs. The layout
// part (terminal height, Actions panel, Source Control view) is a
// preference of the user, not of a project, and stays in session.json.
type SessionState struct {
	// Workspace is the absolute workspace root this session belongs to.
	// Only informative (the file name is a hash of it).
	Workspace    string            `json:"workspace,omitempty"`
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
	// ExplorerWidth is the sidebar width the user dragged (0 = default).
	ExplorerWidth int `json:"explorer_width,omitempty"`

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

	// Group A: the AI chat shown on reopen + its scroll offset (lines up
	// from the bottom).
	AILastChat   string `json:"ai_last_chat,omitempty"`
	AIChatScroll int    `json:"ai_chat_scroll,omitempty"`
}

// sessionPath is the global session.json: layout preferences, plus the
// whole state of a pre-per-workspace termocode until it is migrated.
func sessionPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

// sessionWorkspace returns the workspace root sessions are keyed by: the
// process cwd (main chdirs into the folder given on the command line, and
// Open Recent Workspace re-execs into the new folder). Symlinks are
// resolved so two spellings of one folder share a session.
func sessionWorkspace() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	return filepath.Abs(cwd)
}

// workspaceSessionFile is the per-workspace session file name for root:
// a short hash, so any path makes a safe file name.
func workspaceSessionFile(root string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return hex.EncodeToString(sum[:8]) + ".json"
}

// workspaceSessionPath is sessions/<hash>.json for the current workspace.
func workspaceSessionPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	root, err := sessionWorkspace()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions", workspaceSessionFile(root)), nil
}

// layoutPart / workspacePart split a SessionState into the global layout
// preferences and the per-workspace state.
func layoutPart(s SessionState) SessionState {
	return SessionState{
		TerminalRows:  s.TerminalRows,
		ExplorerWidth: s.ExplorerWidth,
		ActionsOpen:   s.ActionsOpen,
		ActionsPinned: s.ActionsPinned,
		ActionsWidth:  s.ActionsWidth,
		GitViewTree:   s.GitViewTree,
	}
}

func workspacePart(s SessionState) SessionState {
	return SessionState{
		Workspace:    s.Workspace,
		OpenFiles:    s.OpenFiles,
		ActivePath:   s.ActivePath,
		ExpandedDirs: s.ExpandedDirs,
		Cursors:      s.Cursors,
		Roots:        s.Roots,
		AILastChat:   s.AILastChat,
		AIChatScroll: s.AIChatScroll,
	}
}

// hasWorkspaceState reports whether s holds any per-workspace data — true
// for a session.json written before sessions were per workspace.
func hasWorkspaceState(s SessionState) bool {
	return len(s.OpenFiles) > 0 || s.ActivePath != "" || len(s.ExpandedDirs) > 0 ||
		len(s.Cursors) > 0 || len(s.Roots) > 0 || s.AILastChat != ""
}

// legacyBelongsTo reports whether an old global session belongs to the
// workspace root: one of its open files / expanded folders is inside
// root. A session with no paths at all can not be placed, so the first
// workspace that loads it takes it (fallback).
func legacyBelongsTo(s SessionState, root string) bool {
	paths := append(append([]string{s.ActivePath}, s.OpenFiles...), s.ExpandedDirs...)
	hasPath := false
	for _, p := range paths {
		if p == "" {
			continue
		}
		hasPath = true
		if !filepath.IsAbs(p) || pathWithin(p, root) {
			return true
		}
	}
	return !hasPath
}

// pathWithin reports whether p is root or inside it.
func pathWithin(p, root string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func readSessionFile(path string) (SessionState, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SessionState{}, false
	}
	var s SessionState
	if err := json.Unmarshal(data, &s); err != nil {
		return SessionState{}, false
	}
	return s, true
}

func writeSessionFile(path string, s SessionState) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	// saveSession runs on every editor state change; skip the write when
	// nothing changed.
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// loadSession reads the session of the current workspace merged with the
// global layout preferences. Missing/corrupt → empty.
//
// Migration: when the workspace has no session file yet and session.json
// still holds workspace state from an older termocode that belongs to
// this workspace (legacyBelongsTo), that state moves to the workspace
// file and is removed from session.json — once.
//
// Defensive filter: drops `term://...` and `... PREVIEW` paths from the
// open-files / cursors maps. Those are scratch/terminal buffers that an
// older termocode wrote out by accident and should never come back —
// reopening them either fails (the term:// URI is meaningless across
// processes) or clones broken UI state.
func loadSession() SessionState {
	gpath, err := sessionPath()
	if err != nil {
		return SessionState{}
	}
	global, _ := readSessionFile(gpath)
	wpath, err := workspaceSessionPath()
	if err != nil {
		return SessionState{}
	}
	ws, ok := readSessionFile(wpath)
	if !ok && hasWorkspaceState(global) {
		if root, err := sessionWorkspace(); err == nil && legacyBelongsTo(global, root) {
			ws = workspacePart(global)
			ws.Workspace = root
			writeSessionFile(wpath, ws)
			writeSessionFile(gpath, layoutPart(global))
		}
	}
	s := workspacePart(ws)
	l := layoutPart(global)
	s.TerminalRows, s.ActionsOpen, s.ActionsPinned = l.TerminalRows, l.ActionsOpen, l.ActionsPinned
	s.ActionsWidth, s.GitViewTree = l.ActionsWidth, l.GitViewTree
	s.ExplorerWidth = l.ExplorerWidth
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

// saveSession writes s: the workspace part to sessions/<hash>.json, the
// layout part to session.json. Workspace state of another project that
// is still waiting in session.json for migration is kept. Errors are
// swallowed (best-effort).
func saveSession(s SessionState) {
	gpath, err := sessionPath()
	if err != nil {
		return
	}
	wpath, err := workspaceSessionPath()
	if err != nil {
		return
	}
	ws := workspacePart(s)
	if root, err := sessionWorkspace(); err == nil {
		ws.Workspace = root
	}
	writeSessionFile(wpath, ws)
	global, _ := readSessionFile(gpath)
	out := workspacePart(global) // legacy state of another workspace, if any
	l := layoutPart(s)
	out.TerminalRows, out.ActionsOpen, out.ActionsPinned = l.TerminalRows, l.ActionsOpen, l.ActionsPinned
	out.ActionsWidth, out.GitViewTree = l.ActionsWidth, l.GitViewTree
	out.ExplorerWidth = l.ExplorerWidth
	writeSessionFile(gpath, out)
}
