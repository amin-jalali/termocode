package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/nvim"
	"termocode/internal/picker"
	"termocode/internal/toast"
)

// ProblemsMsg carries the result of a fetchProblemsCmd run. The picker is
// rebuilt from this each time the user opens the Problems overlay so the
// list always reflects current LSP state.
type ProblemsMsg struct {
	Items []picker.Item
	// raw entries kept in parallel so handlePickerSelect can decode the
	// chosen ID back to a (path, line, col) triple. We index by ID.
	Index map[string]nvim.Diagnostic
}

// fetchProblemsCmd asynchronously gathers diagnostics from every loaded
// buffer and returns a ProblemsMsg. Items are pre-sorted by severity,
// then file, then line — most actionable first.
func (m Model) fetchProblemsCmd() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	c := m.nvim
	cwd, _ := os.Getwd()
	return func() tea.Msg {
		diags, err := c.AllDiagnostics()
		if err != nil {
			return ErrMsg{Err: err}
		}
		sort.SliceStable(diags, func(i, j int) bool {
			if diags[i].Severity != diags[j].Severity {
				return diags[i].Severity < diags[j].Severity
			}
			if diags[i].Path != diags[j].Path {
				return diags[i].Path < diags[j].Path
			}
			return diags[i].Line < diags[j].Line
		})
		items := make([]picker.Item, 0, len(diags))
		index := make(map[string]nvim.Diagnostic, len(diags))
		for i, d := range diags {
			id := fmt.Sprintf("p-%d", i)
			items = append(items, picker.Item{
				ID:    id,
				Title: formatProblemTitle(d, cwd),
				Hint:  formatProblemHint(d),
			})
			index[id] = d
		}
		return ProblemsMsg{Items: items, Index: index}
	}
}

// openProblemsPicker opens the Problems overlay. The fetch is async so the
// caller schedules fetchProblemsCmd; on receipt of the ProblemsMsg we
// instantiate the picker.
func (m *Model) openProblemsPicker() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	return m.fetchProblemsCmd()
}

// applyProblemsMsg installs the freshly-built picker. Called from update.go
// when ProblemsMsg arrives. Returns a tea.Cmd carrying any toast that needs
// to fire (e.g. "no problems detected").
func (m *Model) applyProblemsMsg(msg ProblemsMsg) tea.Cmd {
	if len(msg.Items) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No problems detected")
		return toastCmd
	}
	m.problemsIndex = msg.Index
	title := fmt.Sprintf(" Problems (%d) ", len(msg.Items))
	m.picker = picker.NewItems(title, msg.Items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindProblems
	return nil
}

// jumpToProblem opens the file at the selected diagnostic's line/col.
func (m *Model) jumpToProblem(id string) {
	d, ok := m.problemsIndex[id]
	if !ok || d.Path == "" {
		return
	}
	if m.nvim != nil {
		m.ensureEditorWindowCurrent()
		// Use `+line` form; col positioning happens inside nvim via :call cursor.
		_ = m.nvim.Command(fmt.Sprintf("edit +%d %s", d.Line, d.Path))
		if d.Col > 0 {
			_ = m.nvim.Command(fmt.Sprintf("call cursor(%d, %d)", d.Line, d.Col))
		}
	}
	m.focus = FocusEditor
}

// formatProblemTitle is the headline shown in the picker — severity glyph +
// message, truncated to keep one line. The file path lives in Hint.
func formatProblemTitle(d nvim.Diagnostic, _ string) string {
	icon := severityIcon(d.Severity)
	msg := strings.SplitN(d.Message, "\n", 2)[0]
	return fmt.Sprintf("%s  %s", icon, msg)
}

// formatProblemHint shows file location + source on the right of the row.
func formatProblemHint(d nvim.Diagnostic) string {
	rel := d.Path
	if cwd, err := os.Getwd(); err == nil {
		if r, err2 := filepath.Rel(cwd, d.Path); err2 == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
	}
	if rel == "" {
		rel = "[no name]"
	}
	loc := fmt.Sprintf("%s:%d", filepath.Base(rel), d.Line)
	if d.Source != "" {
		return loc + "  " + d.Source
	}
	return loc
}

func severityIcon(sev int) string {
	switch sev {
	case 1:
		return "✘" // error
	case 2:
		return "⚠" // warning
	case 3:
		return "ℹ" // info
	case 4:
		return "💡" // hint
	}
	return "·"
}
