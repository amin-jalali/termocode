package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// ── Workflows: keys, palette, Problems rows (WFX-7, plan §5.8) ──────────

// workflowsGuidePage is the guide of the view.
const workflowsGuidePage = "guides/workflows.md"

func (m *Model) wfxMove(d int) {
	st := m.wfxS()
	keys := m.wfxKeys()
	if len(keys) == 0 {
		return
	}
	i := 0
	for j, k := range keys {
		if k == st.sel {
			i = j
		}
	}
	i = min(max(i+d, 0), len(keys)-1)
	st.sel = keys[i]
}

// wfxFilterKey handles the typed filter; true: the key was used.
func (m *Model) wfxFilterKey(k tea.KeyMsg) bool {
	st := m.wfxS()
	if !st.typing {
		return false
	}
	switch k.Type {
	case tea.KeyEsc:
		st.typing, st.filter = false, ""
	case tea.KeyEnter:
		st.typing = false
	case tea.KeyBackspace:
		if r := []rune(st.filter); len(r) > 0 {
			st.filter = string(r[:len(r)-1])
		}
	case tea.KeyRunes, tea.KeySpace:
		st.filter += string(k.Runes)
		if k.Type == tea.KeySpace {
			st.filter += " "
		}
	default:
		return true
	}
	if keys := m.wfxKeys(); len(keys) > 0 {
		found := false
		for _, n := range keys {
			found = found || n == st.sel
		}
		if !found {
			st.sel = keys[0]
		}
	}
	return true
}

// wfxVerb runs the verb keys both the list and the page share.
func (m *Model) wfxVerb(key string) (tea.Cmd, bool) {
	switch key {
	case "r":
		return m.wfxAskRun(false), true
	case "d":
		return m.wfxAskRun(true), true
	case "o":
		return m.wfxAskToggle(), true
	case "e":
		return m.wfxEdit(), true
	case "x":
		return m.wfxAskStop(), true
	case "c":
		return m.wfxChatTarget(), true
	case "n":
		return m.wfxChat(nil), true
	case "?":
		return m.wfxKeyCard(), true
	case "R":
		ws, _ := m.rootDir()
		m.wfxLoad(ws)
		return m.wfxSendProblems(), true
	}
	return nil, false
}

// handleWorkflowsSidebarKey handles a key while the list has the focus.
func (m *Model) handleWorkflowsSidebarKey(k tea.KeyMsg) (Model, tea.Cmd) {
	if m.wfxFilterKey(k) {
		return *m, nil
	}
	st := m.wfxS()
	switch key := k.String(); key {
	case "up", "k":
		m.wfxMove(-1)
	case "down", "j":
		m.wfxMove(1)
	case "enter", " ", "h":
		if st.sel != "" {
			return *m, m.wfxShowPage(st.sel, key != " ")
		}
	case "/":
		st.typing = true
	case "esc":
		m.focus = FocusEditor
	default:
		if cmd, ok := m.wfxVerb(key); ok {
			return *m, cmd
		}
	}
	return *m, nil
}

// routeToWorkflowsPage handles keys while the page has the focus.
func (m Model) routeToWorkflowsPage(msg tea.Msg) (Model, tea.Cmd, bool) {
	k, ok := msg.(tea.KeyMsg)
	if !ok || m.focus != FocusEditor {
		return m, nil, false
	}
	cmd, used := m.handleWorkflowsPageKey(k)
	return m, cmd, used
}

func (m *Model) handleWorkflowsPageKey(k tea.KeyMsg) (tea.Cmd, bool) {
	st := m.wfxS()
	if st.detail == "" && m.wfxFilterKey(k) {
		return nil, true
	}
	key := k.String()
	switch key {
	case "esc":
		switch {
		case strings.HasPrefix(st.detail, "run:"):
			if rf, ok := m.wfxRunByID(strings.TrimPrefix(st.detail, "run:")); ok {
				return m.wfxShowPage(rf.Workflow, true), true
			}
			return m.wfxShowPage("", true), true
		case st.detail != "":
			return m.wfxShowPage("", true), true
		}
		return nil, false
	case "up", "k":
		if st.detail == "" {
			m.wfxMove(-1)
		} else {
			st.pageScroll = max(st.pageScroll-1, 0)
		}
		return nil, true
	case "down", "j":
		if st.detail == "" {
			m.wfxMove(1)
		} else {
			st.pageScroll++
		}
		return nil, true
	case "pgup":
		st.pageScroll = max(st.pageScroll-10, 0)
		return nil, true
	case "pgdown":
		st.pageScroll += 10
		return nil, true
	case "enter":
		if st.detail == "" && st.sel != "" {
			return m.wfxShowPage(st.sel, true), true
		}
		return nil, true
	case "/":
		if st.detail == "" {
			st.typing = true
			return nil, true
		}
	case "l":
		st.pageScroll = 1 << 20 // the bottom: the live output
		return nil, true
	case "h":
		// History: the newest run of the target, else its page.
		if e, ok := m.wfxTarget(); ok {
			if runs := st.runs[e.Name]; len(runs) > 0 && !strings.HasPrefix(st.detail, "run:") {
				return m.wfxShowPage("run:"+runs[0].ID, true), true
			}
			return m.wfxShowPage(e.Name, true), true
		}
		return nil, true
	}
	return m.wfxVerb(key)
}

func wfxKeyRowsList() [][]string {
	return [][]string{
		{"Move"},
		{"↑ / ↓", "move (the list, or scroll a page)"},
		{"Enter", "open the workflow's page"},
		{"/", "filter by name (Esc clears)"},
		{"Esc", "back: run → workflow → overview"},
		{"Workflow"},
		{"r", "run now (asks; a repo file asks for trust first)"},
		{"d", "dry run: read for real, write nothing (asks)"},
		{"o", "schedule on / off (asks)"},
		{"e", "open the file (at the first problem)"},
		{"n", "new workflow by chat (AI: New Workflow…)"},
		{"c", "change it by chat (the file attached)"},
		{"h", "history: the newest run"},
		{"R", "read the files again"},
		{"Run"},
		{"x", "stop the run (asks; Keep running is first)"},
		{"l", "jump to the live output"},
	}
}

func (m *Model) wfxKeyCard() tea.Cmd {
	return m.openKeyCard("Workflows keys", keyRows(wfxKeyRowsList()...), help.Loc{Page: workflowsGuidePage, Anchor: "keys"}, nil)
}

// ── Palette (each id has an AI policy entry) ─────────────────────────────

func workflowsPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "workflows", Title: "Workflows: Open"},
		{ID: "workflow-new", Title: "AI: New Workflow…"},
		{ID: "workflow-run", Title: "Workflows: Run Selected…"},
		{ID: "workflow-dry-run", Title: "Workflows: Dry Run Selected…"},
		{ID: "workflow-enable", Title: "Workflows: Turn Selected On/Off…"},
		{ID: "workflow-stop", Title: "Workflows: Stop Selected Run…"},
		{ID: "workflow-edit-file", Title: "Workflows: Edit Selected File"},
		{ID: "workflow-history", Title: "Workflows: History of Selected"},
	}
}

func (m *Model) dispatchWorkflowsPalette(id string) (tea.Cmd, bool) {
	if !strings.HasPrefix(id, "workflow") {
		return nil, false
	}
	if id == "workflow-new" { // a chat, not the view
		return m.wfxChat(nil), true
	}
	open := m.revealWorkflows(true)
	switch id {
	case "workflows":
		return open, true
	case "workflow-run":
		return tea.Batch(open, m.wfxAskRun(false)), true
	case "workflow-dry-run":
		return tea.Batch(open, m.wfxAskRun(true)), true
	case "workflow-enable":
		return tea.Batch(open, m.wfxAskToggle()), true
	case "workflow-stop":
		return tea.Batch(open, m.wfxAskStop()), true
	case "workflow-edit-file":
		return tea.Batch(open, m.wfxEdit()), true
	case "workflow-history":
		cmd, _ := m.handleWorkflowsPageKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
		return tea.Batch(open, cmd), true
	}
	return nil, false
}

// ── Problems rows for broken files ───────────────────────────────────────

// wfxNamespace is the vim.diagnostic namespace of workflow file problems.
const wfxNamespace = "termocode-workflows"

// wfxProblemsPayload is the setTaskDiagnosticsLua payload: one group per
// file with problems (errors 1, warnings 2).
func wfxProblemsPayload(list []wire.Entry) []any {
	var out []any
	for _, e := range list {
		var rows []any
		for _, p := range e.Problems {
			rows = append(rows, []any{max(p.Line, 1), 1, 1, "workflow: " + wfxProblemText(p.Where, p.Msg)})
		}
		for _, p := range e.Warns {
			rows = append(rows, []any{max(p.Line, 1), 1, 2, "workflow: " + wfxProblemText(p.Where, p.Msg)})
		}
		if len(rows) > 0 {
			out = append(out, []any{e.Path, rows})
		}
	}
	return out
}

func wfxProblemText(where, msg string) string {
	if where == "" {
		return msg
	}
	return where + ": " + msg
}

// wfxSendProblems puts the problems of the workflow files in Problems
// (only when they changed).
func (m *Model) wfxSendProblems() tea.Cmd {
	st := m.wfxS()
	payload := wfxProblemsPayload(st.list)
	sig := fmt.Sprint(payload)
	if sig == st.problemSig || m.nvim == nil {
		return nil
	}
	st.problemSig = sig
	_ = m.nvim.ExecLuaArgs(setTaskDiagnosticsLua, payload, "workflow", wfxNamespace)
	return nil
}
