package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/confirm"
	"termocode/internal/nvim"
	"termocode/internal/toast"
)

// osRemoveAll is wrapped so tests can stub if needed.
var osRemoveAll = os.RemoveAll

type confirmKindEnum int

const (
	confirmKindCloseBuffer confirmKindEnum = iota
	confirmKindDeletePath
	confirmKindGitDiscard
)

// findBuffer returns the buffer info for the given id, if known.
func (m Model) findBuffer(id int) (nvim.BufferInfo, bool) {
	for _, b := range m.bufs {
		if b.ID == id {
			return b, true
		}
	}
	return nvim.BufferInfo{}, false
}

// promptCloseBuffer opens the unsaved-changes confirm dialog for the given id.
// Returns true if a dialog was opened; false if the buffer is clean and the
// caller should proceed with the close.
func (m *Model) promptCloseBufferIfDirty(id int) bool {
	buf, ok := m.findBuffer(id)
	if !ok || !buf.Modified {
		return false
	}
	name := filepath.Base(buf.Path)
	if name == "" {
		name = "[unnamed]"
	}
	m.confirm = confirm.New(
		"Save changes?",
		fmt.Sprintf("\"%s\" has unsaved changes. Do you want to save before closing?", name),
		[]confirm.Button{
			{ID: "save", Title: "Save", Style: confirm.StylePrimary},
			{ID: "discard", Title: "Don't Save", Style: confirm.StyleDestructive},
			{ID: "cancel", Title: "Cancel"},
		},
	)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindCloseBuffer
	m.confirmTargetID = id
	return true
}

func (m *Model) handleConfirmSelect(id string) tea.Cmd {
	switch m.confirmKind {
	case confirmKindCloseBuffer:
		switch id {
		case "cancel":
			return nil
		case "save":
			if buf, ok := m.findBuffer(m.confirmTargetID); ok {
				m.pushClosed(buf.Path)
			}
			if m.nvim != nil {
				_ = m.nvim.Command(fmt.Sprintf("buffer %d | update | bdelete %d", m.confirmTargetID, m.confirmTargetID))
			}
		case "discard":
			if buf, ok := m.findBuffer(m.confirmTargetID); ok {
				m.pushClosed(buf.Path)
			}
			if m.nvim != nil {
				_ = m.nvim.Command(fmt.Sprintf("bdelete! %d", m.confirmTargetID))
			}
		}
	case confirmKindDeletePath:
		switch id {
		case "cancel":
			return nil
		case "delete":
			// Bulk delete: confirmTargetPaths populated; iterate and
			// surface a count-based toast at the end. Single delete:
			// confirmTargetPath populated.
			if len(m.confirmTargetPaths) > 0 {
				return m.runBulkDelete(m.confirmTargetPaths)
			}
			path := m.confirmTargetPath
			if err := osRemoveAll(path); err != nil {
				m.err = err.Error()
				return nil
			}
			m.err = ""
			// Close any open buffer pointing at the just-deleted path so
			// the user doesn't keep editing a phantom file.
			m.closeBuffersForPaths([]string{path})
			m.explorer.Reload()
		}
	case confirmKindGitDiscard:
		if id == "discard" {
			return m.gitDiscardConfirmed()
		}
	}
	return nil
}

// runBulkDelete os.RemoveAll's every path in `paths`, closes any open
// buffers whose path matches a deleted entry (so stale tabs disappear),
// reloads the explorer tree, and surfaces a toast summarising the
// outcome (counts of deleted vs failed). Returns the toast tea.Cmd.
func (m *Model) runBulkDelete(paths []string) tea.Cmd {
	if len(paths) == 0 {
		return nil
	}
	deleted := 0
	var failed []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		if err := osRemoveAll(p); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", filepath.Base(p), err))
			continue
		}
		deleted++
	}
	if deleted > 0 {
		m.closeBuffersForPaths(paths)
	}
	m.explorer.Reload()
	m.explorer.ClearSelection()

	var cmd tea.Cmd
	switch {
	case len(failed) == 0:
		label := fmt.Sprintf("Deleted %d items", deleted)
		if deleted == 1 {
			label = "Deleted 1 item"
		}
		m.toast, cmd = m.toast.Push(toast.Info, label)
		m.err = ""
	case deleted == 0:
		body := strings.Join(failed, "\n")
		m.toast, cmd = m.toast.PushDetail(toast.Errr, "Delete failed", body)
		m.err = body
	default:
		body := strings.Join(failed, "\n")
		m.toast, cmd = m.toast.PushDetail(toast.Warn,
			fmt.Sprintf("Deleted %d, %d failed", deleted, len(failed)),
			body)
		m.err = body
	}
	return cmd
}

// closeBuffersForPaths runs `:bdelete!` against every open buffer whose
// path is in `paths`, so deleting a file from disk doesn't leave a stale
// tab behind. Best-effort: a missing buffer or nvim error is silently
// dropped (the file is gone either way).
func (m *Model) closeBuffersForPaths(paths []string) {
	if m.nvim == nil || len(paths) == 0 {
		return
	}
	target := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		target[p] = struct{}{}
	}
	for _, b := range m.bufs {
		if b.Path == "" {
			continue
		}
		if _, ok := target[b.Path]; !ok {
			// Also match when the deleted path is a directory ancestor
			// of the buffer's file (e.g. user removed `internal/foo/`
			// and there's an open buffer at `internal/foo/bar.go`).
			match := false
			for p := range target {
				if strings.HasPrefix(b.Path, p+string(filepath.Separator)) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		m.pushClosed(b.Path)
		_ = m.nvim.Command(fmt.Sprintf("bdelete! %d", b.ID))
	}
}
