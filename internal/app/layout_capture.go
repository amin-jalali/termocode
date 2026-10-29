package app

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/layout"
)

// Layouts (Group G-LAY): named layout files. This file reads the live
// layout of the IDE into a layout.Layout; layout_apply.go does the reverse.

// layoutViewNames maps a sidebar view to the name used in layout files.
var layoutViewNames = map[activity.View]string{
	activity.ViewFiles:     "files",
	activity.ViewSearch:    "search",
	activity.ViewGit:       "git",
	activity.ViewRun:       "run",
	activity.ViewTests:     "tests",
	activity.ViewServices:  "containers",
	activity.ViewDatabases: "databases",
	activity.ViewItermoc:   "itermoc",
	activity.ViewRuns:      "runs",
}

// layoutPanelNames maps a bottom panel tab kind to the name in layout files.
var layoutPanelNames = map[panelKind]string{
	panelKindTerminal:     "terminal",
	panelKindProblems:     "problems",
	panelKindOutput:       "output",
	panelKindTests:        "tests",
	panelKindDebugConsole: "debug-console",
}

func layoutViewFromName(name string) (activity.View, bool) {
	for v, n := range layoutViewNames {
		if n == name {
			return v, true
		}
	}
	return 0, false
}

func layoutPanelFromName(name string) (panelKind, bool) {
	for k, n := range layoutPanelNames {
		if n == name {
			return k, true
		}
	}
	return 0, false
}

// layoutRelPath is p relative to root, or "" when p is not inside root.
func layoutRelPath(root, p string) string {
	if root == "" || p == "" || !filepath.IsAbs(p) {
		return ""
	}
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

// captureLayout reads the live layout: sidebar, panels, terminal tabs, open
// files and splits. Paths are relative to the project folder, and anything
// outside it is left out, so the file can be shared. Terminal output, the
// shells' environment and secrets are never read.
func (m *Model) captureLayout(name string) layout.Layout {
	l := layout.Layout{Name: name}
	root, _ := m.rootDir()

	sb := &layout.Sidebar{Open: m.showExp && !m.zenMode, View: layoutViewNames[m.activity.Active()], Width: m.explorerWidth}
	l.Sidebar = sb

	chat := &layout.Chat{Open: m.actionsOpen && m.actionsPinned}
	if m.actionsWidthSet {
		chat.Width = m.actionsWidth
	}
	l.Chat = chat

	p := &layout.Panel{Open: m.termOpen}
	if m.termOpen {
		p.Rows = m.terminalRows
		p.Active = layoutPanelNames[m.panelActive]
		for _, k := range m.panelTabs {
			if n, ok := layoutPanelNames[k]; ok {
				p.Tabs = append(p.Tabs, n)
			}
		}
		for _, t := range m.terminalTabs {
			p.Terminals = append(p.Terminals, layout.Terminal{Name: t.Name, Cwd: layoutRelPath(root, t.Cwd), Panes: layoutPanes(t, root)})
		}
	}
	l.Panel = p

	e := &layout.Editor{}
	for _, b := range m.bufs {
		if rel := layoutRelPath(root, b.Path); rel != "" && b.Title == "" {
			e.Files = append(e.Files, rel)
		}
	}
	if m.activeBuf >= 0 {
		for _, b := range m.bufs {
			if b.ID == m.activeBuf {
				e.Active = layoutRelPath(root, b.Path)
			}
		}
	}
	e.Split, e.Panes = m.captureEditorSplit(root)
	l.Editor = e

	switch {
	case m.inTerminal:
		l.Focus = "terminal"
	case m.focus == FocusExplorer:
		l.Focus = "sidebar"
	default:
		l.Focus = "editor"
	}
	return l
}

// layoutPaneSplits maps a pane direction to its name in layout files.
var layoutPaneSplits = map[paneDir]string{paneRight: "right", paneDown: "down"}

// layoutPanes is the split shape of tab t: each extra pane with its
// direction, parent and folder (relative to root, "" when outside it).
func layoutPanes(t terminalTab, root string) []layout.Pane {
	var out []layout.Pane
	for _, s := range t.Splits {
		out = append(out, layout.Pane{Split: layoutPaneSplits[s.Dir], Parent: s.Parent, Cwd: layoutRelPath(root, s.Cwd)})
	}
	return out
}

// captureEditorSplit asks nvim for the editor windows (not the panel's):
// the split direction and the file each one shows. No split gives "" and no
// panes.
func (m *Model) captureEditorSplit(root string) (string, []string) {
	if m.nvim == nil {
		return "", nil
	}
	out, err := m.nvim.EvalLuaString(`
		local wins, rows, cols = {}, {}, {}
		for _, w in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
			local b = vim.api.nvim_win_get_buf(w)
			local cfg = vim.api.nvim_win_get_config(w)
			if cfg.relative == '' and not vim.w[w].termocode_panel and vim.bo[b].buftype == '' then
				local pos = vim.api.nvim_win_get_position(w)
				wins[#wins + 1] = { file = vim.api.nvim_buf_get_name(b), row = pos[1], col = pos[2] }
			end
		end
		table.sort(wins, function(a, b)
			if a.row ~= b.row then return a.row < b.row end
			return a.col < b.col
		end)
		local dir = ''
		if #wins > 1 then
			dir = (wins[1].row == wins[2].row) and 'vertical' or 'horizontal'
		end
		return vim.json.encode({ dir = dir, files = wins })
	`)
	if err != nil {
		return "", nil
	}
	var got struct {
		Dir   string `json:"dir"`
		Files []struct {
			File string `json:"file"`
		} `json:"files"`
	}
	if json.Unmarshal([]byte(out), &got) != nil || got.Dir == "" || len(got.Files) < 2 {
		return "", nil
	}
	panes := make([]string, 0, len(got.Files))
	for _, f := range got.Files {
		panes = append(panes, layoutRelPath(root, f.File))
	}
	return got.Dir, panes
}
