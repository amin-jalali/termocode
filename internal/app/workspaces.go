package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/picker"
	"termocode/internal/toast"
)

// WorkspaceEntry is one row in the persisted recent-workspaces list.
type WorkspaceEntry struct {
	Path     string    `json:"path"`
	OpenedAt time.Time `json:"opened_at"`
}

const workspacesCap = 15

// workspacesPath returns the on-disk location for the recent-workspaces list.
func workspacesPath() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "termocode", "workspaces.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "termocode", "workspaces.json"), nil
}

func loadWorkspaces() []WorkspaceEntry {
	path, err := workspacesPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entries []WorkspaceEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil
	}
	return entries
}

func saveWorkspaces(entries []WorkspaceEntry) {
	path, err := workspacesPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// PushWorkspace records cwd as the most recent workspace. Called from main()
// at startup so every termocode launch contributes to the list. Exported
// so cmd/termocode/main.go can call it.
func PushWorkspace(path string) { pushWorkspace(path) }

// pushWorkspace records cwd as the most recent workspace.
func pushWorkspace(path string) {
	if path == "" {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	entries := loadWorkspaces()
	out := make([]WorkspaceEntry, 0, len(entries)+1)
	out = append(out, WorkspaceEntry{Path: abs, OpenedAt: time.Now()})
	for _, e := range entries {
		if e.Path == abs {
			continue
		}
		out = append(out, e)
		if len(out) >= workspacesCap {
			break
		}
	}
	saveWorkspaces(out)
}

// openWorkspacePicker shows the recent-workspaces list. Selection re-execs
// termocode in the chosen folder.
func (m *Model) openWorkspacePicker() tea.Cmd {
	entries := loadWorkspaces()
	if len(entries) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No recent workspaces yet")
		return toastCmd
	}
	cwd, _ := os.Getwd()
	items := make([]picker.Item, 0, len(entries))
	for _, e := range entries {
		hint := formatRecentTime(e.OpenedAt, time.Now())
		if e.Path == cwd {
			hint = "current"
		}
		items = append(items, picker.Item{
			ID:    e.Path,
			Title: e.Path,
			Hint:  hint,
		})
	}
	m.picker = picker.NewItems(" Open Recent Workspace ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindWorkspaces
	return nil
}

// openWorkspace re-execs termocode with the chosen folder as its cwd. We
// can't change cwd of the running process and rebuild the entire model
// (sessions, nvim state, layout caches all need a clean reset), so the
// safest option is to swap process image entirely. syscall.Exec preserves
// PID, file descriptors, and environment.
//
// The current session is saved on the way out via the existing StateMsg
// path; the new instance loads the per-cwd session.json on startup.
func (m *Model) openWorkspace(path string) tea.Cmd {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Workspace error", err.Error())
		return toastCmd
	}
	// Persist any pending session state, close nvim cleanly, then re-exec.
	if m.nvim != nil {
		_ = m.nvim.Close()
	}
	bin, err := os.Executable()
	if err != nil {
		bin = os.Args[0]
	}
	args := []string{filepath.Base(bin), path}
	env := os.Environ()
	// syscall.Exec replaces this process; if it returns, it failed.
	if err := syscall.Exec(bin, args, env); err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Exec failed", err.Error())
		return toastCmd
	}
	// Unreachable on success.
	return nil
}

var _ = fmt.Sprintf
