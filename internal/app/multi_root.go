package app

import (
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/picker"
	"termocode/internal/prompt"
	"termocode/internal/toast"
)

// Multi-root workspace plumbing for the app layer.
//
// The explorer ships the actual tree management; this file is just the
// glue between palette commands, the path-prompt overlay, the persistence
// file, and the explorer's AddRoot / RemoveRoot / Roots methods.

// openAddRootPrompt asks the user for a directory path. We reuse the simple
// prompt overlay rather than the file picker since the file picker is
// file-only (no dir filter today) and a typed/pasted path is the fastest
// path for "I know where the folder is".
func (m *Model) openAddRootPrompt() {
	initial := ""
	if home, err := os.UserHomeDir(); err == nil {
		initial = home + string(filepath.Separator)
	}
	m.prompt = prompt.New("Add Folder to Workspace", "Folder path:", initial)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindAddRoot
}

// addRootFromPrompt validates the user-entered path, hands it to the
// explorer, then persists the new roots list. Errors surface as a toast so
// the user gets feedback (tilde expansion failure, missing dir, file not
// dir, etc.) without losing their typed input.
func (m *Model) addRootFromPrompt(value string) tea.Cmd {
	expanded, err := expandUser(value)
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Add folder failed", err.Error())
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Add folder failed", err.Error())
	}
	info, err := os.Stat(abs)
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Add folder failed", err.Error())
	}
	if !info.IsDir() {
		return m.pushToastDetail(toast.Errr, "Add folder failed", "not a directory")
	}
	if err := m.explorer.AddRoot(abs); err != nil {
		return m.pushToastDetail(toast.Errr, "Add folder failed", err.Error())
	}
	m.persistWorkspaceRoots()
	return m.pushToastDetail(toast.Info, "Added", filepath.Base(abs))
}

// openRemoveRootPicker pops a sub-picker listing every root currently in the
// workspace. The primary (cwd-based) root is shown but greyed out — its
// selection is rejected because git, terminal, and so on are pinned to it.
// Toast surfaces a helpful message if there's nothing extra to remove.
func (m *Model) openRemoveRootPicker() tea.Cmd {
	roots := m.explorer.Roots()
	primary := m.explorer.PrimaryRoot()
	if len(roots) <= 1 {
		return m.pushToast(toast.Info, "No additional folders to remove")
	}
	items := make([]picker.Item, 0, len(roots))
	for _, r := range roots {
		hint := ""
		if r == primary {
			hint = "primary (cannot remove)"
		}
		items = append(items, picker.Item{ID: r, Title: r, Hint: hint})
	}
	m.picker = picker.NewItems(" Remove Folder from Workspace ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindRemoveRoot
	return m.picker.Init()
}

// removeRootFromPicker drops the selected root, persists, and toasts.
// Selecting the primary root is refused (with feedback).
func (m *Model) removeRootFromPicker(id string) tea.Cmd {
	if id == "" {
		return nil
	}
	if id == m.explorer.PrimaryRoot() {
		return m.pushToast(toast.Errr, "Primary folder cannot be removed")
	}
	if !m.explorer.RemoveRoot(id) {
		return m.pushToast(toast.Errr, "Folder not in workspace")
	}
	m.persistWorkspaceRoots()
	return m.pushToastDetail(toast.Info, "Removed", filepath.Base(id))
}

// persistWorkspaceRoots writes the current set of *extra* roots back to the
// session file. The primary root is NOT persisted — it's whatever cwd the
// user launched termocode in (already handled elsewhere). Best-effort: a
// write error is swallowed by saveSession.
func (m *Model) persistWorkspaceRoots() {
	all := m.explorer.Roots()
	primary := m.explorer.PrimaryRoot()
	extras := make([]string, 0, len(all))
	for _, r := range all {
		if r == primary {
			continue
		}
		extras = append(extras, r)
	}
	existing := loadSession()
	existing.Roots = extras
	saveSession(existing)
}

// loadAndApplyWorkspaceRoots reads session.Roots and feeds each surviving
// (still-on-disk) entry into the explorer. Roots whose path no longer exists
// are silently dropped — and the cleaned list written back so the next run
// doesn't try to load them again.
func (m *Model) loadAndApplyWorkspaceRoots() {
	sess := loadSession()
	if len(sess.Roots) == 0 {
		return
	}
	primary := m.explorer.PrimaryRoot()
	kept := make([]string, 0, len(sess.Roots))
	for _, p := range sess.Roots {
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		// Skip if it's the same as the primary cwd root (would dedup anyway,
		// but cleaner not to keep it in the persisted extras list).
		if abs == primary {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			// Path no longer exists / isn't a dir — drop it.
			continue
		}
		if err := m.explorer.AddRoot(abs); err != nil {
			continue
		}
		kept = append(kept, abs)
	}
	if len(kept) != len(sess.Roots) {
		// Persist the cleaned-up list so the next run starts fresh.
		sess.Roots = kept
		saveSession(sess)
	}
}

// expandUser expands a leading "~" or "~/..." into the user's home dir.
// Anything else is returned unchanged.
func expandUser(p string) (string, error) {
	if p == "" {
		return p, nil
	}
	if p[0] != '~' {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if p == "~" {
		return home, nil
	}
	if len(p) >= 2 && (p[1] == '/' || p[1] == filepath.Separator) {
		return filepath.Join(home, p[2:]), nil
	}
	return p, nil
}

// pushToast is a thin helper around m.toast.Push that returns the resulting
// tea.Cmd directly so action handlers can `return m.pushToast(...)`.
func (m *Model) pushToast(level toast.Severity, msg string) tea.Cmd {
	var cmd tea.Cmd
	m.toast, cmd = m.toast.Push(level, msg)
	return cmd
}

// pushToastDetail is the two-line variant of pushToast. Use it when there's
// a clear "what + context" split so the toast can render as bold title +
// muted detail rather than a single long line.
func (m *Model) pushToastDetail(level toast.Severity, title, detail string) tea.Cmd {
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushDetail(level, title, detail)
	return cmd
}

