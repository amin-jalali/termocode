package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/confirm"
	"termocode/internal/prompt"
)

type promptKindEnum int

const (
	promptKindRename promptKindEnum = iota
	promptKindNewFile
	promptKindNewFolder
	promptKindCommit
	promptKindGitNewBranch
	promptKindGotoLine
	promptKindReplaceFind
	promptKindReplaceReplacement
	promptKindCompareRevision
	promptKindAddRoot
	promptKindSetting
	promptKindThemeColor
	promptKindKeybinding
	promptKindFindInFiles
)

func (m *Model) openRenamePrompt(path string) {
	m.prompt = prompt.New("Rename", "New name:", filepath.Base(path))
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindRename
	m.promptPath = path
}

func (m *Model) openNewFilePrompt(dir string) {
	m.prompt = prompt.New("New File", "Filename:", "")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindNewFile
	m.promptPath = dir
}

func (m *Model) openNewFolderPrompt(dir string) {
	m.prompt = prompt.New("New Folder", "Folder name:", "")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindNewFolder
	m.promptPath = dir
}

func (m *Model) openDeleteConfirm(path string) {
	name := filepath.Base(path)
	m.confirm = confirm.New(
		"Delete?",
		fmt.Sprintf("Are you sure you want to permanently delete \"%s\"?", name),
		[]confirm.Button{
			{ID: "delete", Title: "Delete", Style: confirm.StyleDestructive},
			{ID: "cancel", Title: "Cancel"},
		},
	)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDeletePath
	m.confirmTargetPath = path
	m.confirmTargetPaths = nil
}

// openBulkDeleteConfirm opens the destructive confirm modal for deleting
// every path in `paths`. The body lists up to 5 basenames followed by an
// ellipsis count. The Yes branch in handleConfirmSelect iterates the list
// and runs os.RemoveAll, then :bdelete!'s any open buffer whose path was
// just removed and refreshes the explorer tree.
//
// Empty `paths` is a no-op (caller's responsibility to fall back to the
// single-path flow when there's no multi-selection).
func (m *Model) openBulkDeleteConfirm(paths []string) {
	if len(paths) == 0 {
		return
	}
	if len(paths) == 1 {
		m.openDeleteConfirm(paths[0])
		return
	}
	// Stable order so the same input always renders the same preview.
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)

	const previewN = 5
	var lines []string
	lines = append(lines, fmt.Sprintf("Delete %d items?", len(sorted)))
	lines = append(lines, "")
	for i, p := range sorted {
		if i >= previewN {
			break
		}
		lines = append(lines, "  "+filepath.Base(p))
	}
	if extra := len(sorted) - previewN; extra > 0 {
		lines = append(lines, fmt.Sprintf("  …and %d more", extra))
	}
	lines = append(lines, "")
	lines = append(lines, "This action cannot be undone.")

	m.confirm = confirm.New(
		"Delete?",
		strings.Join(lines, "\n"),
		[]confirm.Button{
			{ID: "delete", Title: fmt.Sprintf("Delete %d", len(sorted)), Style: confirm.StyleDestructive},
			{ID: "cancel", Title: "Cancel"},
		},
	)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDeletePath
	m.confirmTargetPath = ""
	m.confirmTargetPaths = sorted
}

// handlePromptSubmit performs the file op for the active prompt kind.
func (m *Model) handlePromptSubmit(value string) tea.Cmd {
	switch m.promptKind {
	case promptKindRename:
		dir := filepath.Dir(m.promptPath)
		newPath := filepath.Join(dir, value)
		if err := os.Rename(m.promptPath, newPath); err != nil {
			m.err = err.Error()
			return nil
		}
		m.err = ""
		m.explorer.Reload()
	case promptKindNewFile:
		newPath := filepath.Join(m.promptPath, value)
		if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
			m.err = err.Error()
			return nil
		}
		f, err := os.OpenFile(newPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
		if err != nil {
			m.err = err.Error()
			return nil
		}
		_ = f.Close()
		m.err = ""
		m.explorer.Reload()
		// open the new file
		m.ensureEditorWindowCurrent()
		if err := m.editor.Open(newPath); err == nil {
			m.focus = FocusEditor
		}
	case promptKindNewFolder:
		newPath := filepath.Join(m.promptPath, value)
		if err := os.MkdirAll(newPath, 0o755); err != nil {
			m.err = err.Error()
			return nil
		}
		m.err = ""
		m.explorer.Reload()
	case promptKindCommit:
		return m.gitCommitFromPrompt(value)
	case promptKindGitNewBranch:
		return m.gitCreateBranchFromPrompt(value)
	case promptKindGotoLine:
		return m.gotoLineFromPrompt(value)
	case promptKindReplaceFind:
		if value != "" {
			m.continueReplaceWithReplacement(value)
		}
	case promptKindReplaceReplacement:
		return m.runReplaceInWorkspace(value)
	case promptKindCompareRevision:
		return m.runCompareWithRevision(value)
	case promptKindAddRoot:
		return m.addRootFromPrompt(value)
	case promptKindSetting:
		return m.applySettingValue(value)
	case promptKindThemeColor:
		return m.applyThemeColor(value)
	case promptKindKeybinding:
		return m.applyKeybinding(value)
	case promptKindFindInFiles:
		return m.runFindInFiles(value)
	}
	return nil
}

// openGotoLinePrompt asks the user for a line number and (after submit)
// runs `:N` in nvim. Accepts plain integers; non-numeric input is rejected
// silently — the user can just close the prompt.
func (m *Model) openGotoLinePrompt() {
	m.prompt = prompt.New("Go to Line", "Line number:", "")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindGotoLine
}

// gotoLineFromPrompt parses the prompt value as a 1-based line number and
// asks nvim to jump there. We use `call cursor()` rather than `:N` so the
// command is filetype-independent and respects the current window.
func (m *Model) gotoLineFromPrompt(value string) tea.Cmd {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 1 {
		return nil
	}
	if m.nvim != nil {
		_ = m.nvim.Command(fmt.Sprintf("call cursor(%d, 1)", n))
	}
	m.focus = FocusEditor
	return nil
}
