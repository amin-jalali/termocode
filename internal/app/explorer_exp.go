package app

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/explorer"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

type bulkRenameState struct {
	bufferStamp string
	paths       []string
	plan        explorer.RenamePlan
	before      map[string]string
	undo        []explorer.Rename
	stamps      map[string]string
	serial      uint64
}
type undoExplorerRenameMsg struct{ Serial uint64 }

func (m *Model) toggleExplorerPreview() tea.Cmd {
	cmd := m.explorer.TogglePreview()
	if m.explorer.PreviewEnabled() {
		m.explorerPreviewWidth = m.explorerWidth
		if m.w >= 100 {
			m.explorerWidth = min(76, m.w-45)
		}
	} else if m.explorerPreviewWidth > 0 {
		m.explorerWidth = m.explorerPreviewWidth
	}
	m.applyLayout()
	return tea.Batch(cmd, m.explorer.SchedulePreview())
}
func (m *Model) renameExplorerSelection() {
	paths := m.explorer.Selection()
	if len(paths) < 2 {
		if p := m.explorer.CursorPath(); p != "" {
			m.openRenamePrompt(p)
		}
		return
	}
	if _, ok := m.remoteFS(); ok { // a container: bulk rename is local only
		m.err = "Rename the files one by one in a container."
		m.toast, _ = m.toast.PushDetail(toast.Warn, "Rename many files", m.err)
		return
	}
	sort.Strings(paths)
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = filepath.Base(p)
	}
	m.bulkRename.paths = paths
	m.prompt = prompt.New("Rename selected files", "", strings.Join(names, "\n"))
	m.prompt.SetMultiline()
	m.prompt.SetSize(m.w, m.h)
	m.promptKind = promptKindBulkRename
	m.promptOpen = true
}
func (m *Model) previewBulkRename(value string) tea.Cmd {
	paths := m.bulkRename.paths
	plan := explorer.CheckRename(paths, strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n"))
	for _, p := range paths {
		for _, root := range m.explorer.Roots() {
			if p == root {
				plan.Conflicts = append(plan.Conflicts, "A workspace folder cannot be renamed")
			}
		}
	}
	m.bulkRename.before = map[string]string{}
	for _, p := range paths {
		stamp, err := explorer.RenameStamp(p)
		if err != nil {
			plan.Conflicts = append(plan.Conflicts, err.Error())
		}
		m.bulkRename.before[p] = stamp
	}
	m.bulkRename.plan = plan
	var lines []string
	for _, c := range plan.Changes {
		lines = append(lines, filepath.Base(c.Old)+" → "+filepath.Base(c.New))
	}
	if len(lines) == 0 {
		lines = append(lines, "No names changed")
	}
	for _, c := range plan.Conflicts {
		lines = append(lines, "Conflict: "+c)
	}
	var actions []confirm.Button
	if len(plan.Conflicts) == 0 && len(plan.Changes) > 0 {
		actions = append(actions, confirm.Button{ID: "apply", Title: "Apply", Style: confirm.StylePrimary})
	}
	m.confirm = confirm.Risky("Rename preview", strings.Join(lines, "\n"), confirm.Button{ID: "cancel", Title: "Cancel"}, actions...).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindBulkRename
	return nil
}
func (m *Model) applyBulkRename() tea.Cmd {
	state := &m.bulkRename
	if len(state.plan.Conflicts) > 0 || len(state.plan.Changes) == 0 {
		return nil
	}
	for p, before := range state.before {
		now, err := explorer.RenameStamp(p)
		if err != nil || now != before {
			return m.pushToastDetail(toast.Warn, "Rename stopped", "Files changed. Open Rename again.")
		}
	}
	if err := explorer.ApplyRename(state.plan.Changes, m.followRename); err != nil {
		m.explorer.Reload()
		return m.pushToastDetail(toast.Errr, "Rename failed", err.Error())
	}
	state.undo = nil
	state.stamps = map[string]string{}
	state.serial++
	for _, c := range state.plan.Changes {
		state.undo = append(state.undo, explorer.Rename{Old: c.New, New: c.Old})
		stamp, err := explorer.RenameStamp(c.New)
		if err == nil {
			state.stamps[c.New] = stamp
		}
	}
	state.bufferStamp = m.renameBufferStamp(state.undo)
	m.explorer.ClearSelection()
	m.explorer.Reload()
	if len(state.plan.Changes) > 0 {
		m.explorer.RevealPath(state.plan.Changes[0].New)
	}
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushClick(toast.Info, fmt.Sprintf("Renamed %d files · Undo", len(state.undo)), "Click to undo", undoExplorerRenameMsg{state.serial})
	return tea.Batch(cmd, m.refreshSearchResults(), m.explorer.SchedulePreview())
}
func (m *Model) undoBulkRename(serial uint64) tea.Cmd {
	state := &m.bulkRename
	if serial != state.serial || len(state.undo) == 0 {
		return nil
	}
	if now := m.renameBufferStamp(state.undo); now != state.bufferStamp {
		return m.pushToastDetail(toast.Warn, "Cannot undo rename", "Open files changed since rename.")
	}
	for _, c := range state.undo {
		stamp, err := explorer.RenameStamp(c.Old)
		if err != nil || stamp != state.stamps[c.Old] {
			return m.pushToastDetail(toast.Warn, "Cannot undo rename", "Files changed since rename.")
		}
	}
	if err := explorer.ApplyRename(state.undo, m.followRename); err != nil {
		return m.pushToastDetail(toast.Warn, "Cannot undo rename", err.Error())
	}
	state.undo = nil
	state.serial++
	m.explorer.Reload()
	return tea.Batch(m.pushToastDetail(toast.Info, "Rename undone", ""), m.refreshSearchResults(), m.explorer.SchedulePreview())
}

// renameBufferStamp also protects edits that have not reached disk yet.
func (m *Model) renameBufferStamp(changes []explorer.Rename) string {
	if m.nvim == nil {
		return ""
	}
	paths := make([]string, len(changes))
	for i, c := range changes {
		paths[i] = c.Old
	}
	stamp, err := m.nvim.EvalLuaStringArgs(`local paths=...; local rows={}
for _,b in ipairs(vim.api.nvim_list_bufs()) do
 local name=vim.api.nvim_buf_get_name(b)
 for _,p in ipairs(paths) do
  if (name==p or name:sub(1,#p+1)==p..'/') and vim.api.nvim_buf_is_loaded(b) then
   rows[#rows+1]={b,name,vim.bo[b].modified,vim.api.nvim_buf_get_lines(b,0,-1,false)}
   break
  end
 end
end
return vim.fn.sha256(vim.json.encode(rows))`, paths)
	if err != nil {
		return "unavailable"
	}
	return stamp
}
