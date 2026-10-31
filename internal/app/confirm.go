package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/toast"
)

// osRemoveAll is wrapped so tests can stub if needed.
var osRemoveAll = os.RemoveAll

type confirmKindEnum int

const (
	confirmKindCloseBuffer confirmKindEnum = iota
	confirmKindDeletePath
	confirmKindGitDiscard
	confirmKindGitStageCommit
	confirmKindToolInstall // LSP / DAP installer (lsp_manager.go)
	confirmKindReplaceWorkspace
	// Group H — keymap conflict, settings import, user snippets.
	confirmKindKeybindingConflict
	confirmKindSettingsImport
	confirmKindSnippetAction
	confirmKindQuit       // unsaved files on quit (quit.go)
	confirmKindHotExit    // restore unsaved files of a crashed run (hot_exit.go)
	confirmKindNvimExited // nvim stopped on its own: Restart / Quit (nvim_exit.go)
	// Download / Upload (transfer.go).
	confirmKindTransferLarge
	confirmKindTransferFallback
	// Commit view (git_commit_view.go).
	confirmKindCommitRevert
	confirmKindCommitCherryPick
	// Discard lines (git_lines.go).
	confirmKindGitDiscardLines
	confirmKindBulkRename
	confirmKindRebaseStart    // git_rebase.go
	confirmKindRebaseControl  // Continue / Skip / Abort a stopped rebase
	confirmKindRebaseFixup    // fixup staged changes into a commit (git_fixup.go)
	confirmKindGitUndo        // undo a git action with the reflog (git_undo.go)
	confirmKindMergeWholeFile // keep ours / theirs or delete (git_merge_view.go)
	confirmKindHotExitDisk    // a restored file changed on disk (hot_exit_disk.go)
	confirmKindWorkspaceGone  // a recent workspace folder is gone (workspaces.go)
	confirmKindCrash          // the last run crashed (crash_ui.go)
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

// liveBuffer is findBuffer with the path and modified flag read from nvim
// itself. m.bufs lags up to one state refresh behind: a Ctrl+W right
// after typing saw a clean buffer, so no question was asked and the
// :bdelete failed on the modified buffer - the key did nothing.
func (m Model) liveBuffer(id int) (nvim.BufferInfo, bool) {
	if m.nvim == nil {
		return m.findBuffer(id)
	}
	s, err := m.nvim.EvalLuaStringArgs(`local b = ...
if not vim.api.nvim_buf_is_valid(b) then return '' end
return (vim.bo[b].modified and '1' or '0') .. vim.api.nvim_buf_get_name(b)`, id)
	if err != nil {
		return m.findBuffer(id)
	}
	if s == "" {
		return nvim.BufferInfo{}, false
	}
	buf, _ := m.findBuffer(id)
	buf.ID, buf.Modified, buf.Path = id, s[0] == '1', s[1:]
	return buf, true
}

// currentBufferID is the id of the buffer in nvim's current window (the
// one :bdelete closes), or the last known active buffer without nvim.
func (m Model) currentBufferID() int {
	if m.nvim != nil {
		if s, err := m.nvim.EvalLuaString(`return tostring(vim.api.nvim_get_current_buf())`); err == nil {
			if id, err := strconv.Atoi(s); err == nil {
				return id
			}
		}
	}
	return m.activeBuf
}

// promptCloseBuffer opens the unsaved-changes confirm dialog for the given id.
// Returns true if a dialog was opened; false if the buffer is clean and the
// caller should proceed with the close.
func (m *Model) promptCloseBufferIfDirty(id int) bool {
	buf, ok := m.liveBuffer(id)
	if !ok || !buf.Modified {
		return false
	}
	name := "[unnamed]" // filepath.Base("") would say "."
	if buf.Path != "" {
		name = filepath.Base(buf.Path)
	}
	if t := m.databaseConsoleTitle(buf.Path); t != "" {
		name = t // "SQL console · local-pg", not the hash of its file name
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

// answerCloseBuffer answers the close-buffer card (Save / Don't Save /
// Cancel). The save-conflict card calls it too after "Overwrite", so the
// close goes on without passing through handleConfirmSelect.
func (m *Model) answerCloseBuffer(id string) tea.Cmd {
	switch id {
	case "cancel":
		m.closeQueue = nil
		return nil
	case "save":
		if m.askSaveConflict(m.confirmTargetID, saveAndClose) {
			return nil
		}
		if m.nvim != nil {
			if err := m.nvim.Command(fmt.Sprintf("buffer %d | update | bdelete %d", m.confirmTargetID, m.confirmTargetID)); err != nil {
				m.closeQueue = nil
				return m.pushToastDetail(toast.Errr, "Save failed", err.Error())
			}
		}
		if buf, ok := m.findBuffer(m.confirmTargetID); ok {
			m.pushClosed(buf.Path)
		}
	case "discard":
		if buf, ok := m.findBuffer(m.confirmTargetID); ok {
			m.pushClosed(buf.Path)
		}
		if m.nvim != nil {
			_ = m.nvim.Command(discardBufferCmd(m.confirmTargetID))
		}
	}
	m.drainCloseQueue() // Close Others / to the Right go on
	return nil
}

func (m *Model) handleConfirmSelect(id string) tea.Cmd {
	if m.confirmKind == confirmKindKeyCard {
		return m.answerKeyCard(id)
	}
	if cmd, ok := m.handleDBConfirm(id); ok {
		return cmd
	}
	if cmd, ok := m.handleLayoutConfirm(id); ok { // layout_palette.go
		return cmd
	}
	if cmd, ok := m.handleAIConfirm(id); ok { // Group A
		return cmd
	}
	if cmd, ok := m.handleSetupConfirm(id); ok { // setup card: GNOME profile
		return cmd
	}
	if cmd, ok := m.handleServicesConfirm(id); ok { // services_actions.go
		return cmd
	}
	if cmd, ok := m.handleKubeConfirm(id); ok { // kube_restart.go
		return cmd
	}
	if cmd, ok := m.handleHTTPConfirm(id); ok { // HTTP client
		return cmd
	}
	if cmd, ok := m.handleImportConfirm(id); ok { // import_apply.go
		return cmd
	}
	if cmd, ok := m.handleCIConfirm(id); ok { // ci_card.go
		return cmd
	}
	if cmd, ok := m.handleUpdateConfirm(id); ok { // update_card.go
		return cmd
	}
	if cmd, ok := m.handlePRConfirm(id); ok { // pr_view.go
		return cmd
	}
	if cmd, ok := m.handleSaveConflictConfirm(id); ok { // save_conflict.go
		return cmd
	}
	switch m.confirmKind {
	case confirmKindBulkRename:
		if id == "apply" {
			return m.applyBulkRename()
		}
		return nil
	case confirmKindCommitRevert, confirmKindCommitCherryPick:
		return m.runCommitPick(id)
	case confirmKindRebaseStart:
		return m.onRebaseConfirm(id)
	case confirmKindRebaseControl:
		return m.onRebaseControl(id)
	case confirmKindRebaseFixup:
		return m.onFixupConfirm(id)
	case confirmKindGitUndo:
		return m.onUndoConfirm(id)
	case confirmKindMergeWholeFile:
		return m.onMergeWholeCard(id)
	case confirmKindCloseBuffer:
		return m.answerCloseBuffer(id)
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
			if fsys, ok := m.remoteFS(); ok { // a container: off the UI loop (FIX-2)
				return m.remoteDelete(fsys, []string{path}, false)
			}
			return m.finishDelete(path, m.removePath(path))
		}
	case confirmKindGitDiscard:
		if id == "discard" {
			return m.gitDiscardConfirmed()
		}
	case confirmKindGitDiscardLines:
		if id == "discard" {
			return m.gitDiscardLinesConfirmed()
		}
	case confirmKindGitStageCommit:
		if id == "stage" {
			return m.gitStageAllAndCommit()
		}
	case confirmKindToolInstall:
		return m.onToolConfirm(id)
	case confirmKindReplaceWorkspace:
		if id == "replace" {
			return m.applyReplaceInWorkspace()
		}
		m.replaceFind, m.replaceRepl, m.replaceTargets = "", "", nil
	// Group H — keymap conflict, settings import, user snippets.
	case confirmKindKeybindingConflict:
		return m.onKeybindingConflictConfirm(id)
	case confirmKindSettingsImport:
		return m.onSettingsImportConfirm(id)
	case confirmKindSnippetAction:
		return m.onSnippetActionConfirm(id)
	case confirmKindQuit:
		return m.onQuitConfirm(id)
	case confirmKindHotExit:
		return m.onHotExitConfirm(id)
	case confirmKindHotExitDisk:
		return m.onHotDiskConfirm(id)
	case confirmKindWorkspaceGone:
		return m.onWorkspaceGoneConfirm(id)
	case confirmKindCrash:
		return m.onCrashConfirm(id)
	case confirmKindNvimExited:
		return m.onNvimExitedConfirm(id)
	case confirmKindTransferLarge, confirmKindTransferFallback:
		return m.onXferConfirm(m.confirmKind, id)
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
	if fsys, ok := m.remoteFS(); ok { // a container: off the UI loop (FIX-2)
		return m.remoteDelete(fsys, paths, true)
	}
	deleted, failed, gone := removePaths(paths, m.removePath)
	return m.finishBulkDelete(deleted, failed, gone)
}

// finishDelete is the end of a single delete: on success the open tabs
// of path close and the explorer and results drop it.
func (m *Model) finishDelete(path string, err error) tea.Cmd {
	if err != nil {
		// The file is still there: its tab (maybe with unsaved
		// edits) stays open.
		m.err = fileOpFailText(err)
		return m.pushToastDetail(toast.Errr, "Delete failed", m.err)
	}
	// Close any open buffer pointing at the path, so the user
	// doesn't keep editing a phantom file. discardBufferCmd
	// marks it unmodified first, so closing it (auto-save on
	// BufLeave) can't write it back.
	m.closeBuffersForPaths([]string{path})
	m.forgetPaths([]string{path})
	m.err = ""
	m.explorer.Reload()
	return m.refreshSearchResults() // drop the deleted file (N2)
}

// removePaths removes each path with remove: how many went, the failures
// as "name: reason" lines, and the paths that went.
func removePaths(paths []string, remove func(string) error) (deleted int, failed, gone []string) {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if err := remove(p); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %s", filepath.Base(p), fileOpFailText(err)))
			continue
		}
		deleted++
		gone = append(gone, p)
	}
	return deleted, failed, gone
}

// finishBulkDelete is the end of a bulk delete: the tabs of what went
// close, the tree reloads and one toast counts the outcome.
func (m *Model) finishBulkDelete(deleted int, failed, gone []string) tea.Cmd {
	// Close the buffers of what is gone (a failed delete keeps its tab
	// and unsaved edits). discardBufferCmd marks them unmodified first,
	// so the auto-save on BufLeave cannot write a deleted file back.
	m.closeBuffersForPaths(gone)
	m.forgetPaths(gone)
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
	if deleted > 0 {
		cmd = tea.Batch(cmd, m.refreshSearchResults()) // drop them from the results (N2)
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
		_ = m.nvim.Command(discardBufferCmd(b.ID))
	}
}

// discardBufferCmd is the Ex command that closes buffer id and drops its
// unsaved edits. The buffer is marked unmodified first: closing the
// current buffer fires BufLeave, whose auto-save would otherwise write the
// edits the user chose not to save (Don't Save saved the file).
func discardBufferCmd(id int) string {
	return fmt.Sprintf("call setbufvar(%d, '&modified', 0) | bdelete! %d", id, id)
}
