package app

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/suggest"
)

// CMP-44: Tasks: Suggested Tasks… reads the workspace for small jobs
// (marker comments such as TODO, FIXME, HACK, PERF, and Go code that is
// likely slow) and shows them as a pick list, most useful first. A row opens a
// Cancel-first card: Open in Editor, or Ask AI to Do It, which only
// fills the chat box (the user reads it and sends it with Enter).
//
// It uses the GITX hub's shared picker and card (gitx_hub.go), so it
// needs no kind numbers of its own.

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "tasks-suggest", Title: "Tasks: Suggested Tasks…", Hint: "TODO / FIXME comments and slow code"},
		},
		policies: map[string]aiActionPolicy{
			"tasks-suggest": aiNever(aiNoPicker),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			if id == "tasks-suggest" {
				return m.openSuggestedTasks(), true
			}
			return nil, false
		},
	})
}

// openSuggestedTasks scans the workspace off the UI loop, then opens
// the list.
func (m *Model) openSuggestedTasks() tea.Cmd {
	root, err := m.rootDir()
	if err != nil || root == "" {
		return m.gitxInfo("Open a folder first: Suggested Tasks reads its files")
	}
	return tea.Batch(m.gitxInfo("Looking for suggested tasks…"), func() tea.Msg {
		res, err := suggest.Scan(root, suggest.Options{})
		return gitxDoneMsg{apply: func(m *Model) tea.Cmd {
			if err != nil {
				return m.gitxFail("Suggested Tasks could not read the folder", err)
			}
			return m.showSuggestedTasks(root, res, "")
		}}
	})
}

// showSuggestedTasks opens the list; cursor is the row to select ("" =
// the first).
func (m *Model) showSuggestedTasks(root string, res suggest.Result, cursor string) tea.Cmd {
	if len(res.Tasks) == 0 {
		return m.gitxInfo("No suggested tasks: no TODO / FIXME comments or slow code found")
	}
	items := make([]picker.Item, len(res.Tasks))
	for i, t := range res.Tasks {
		items[i] = picker.Item{ID: strconv.Itoa(i), Title: suggestedTaskTitle(t), Hint: t.Where()}
	}
	title := fmt.Sprintf(" Suggested Tasks (%d) ", len(res.Tasks))
	m.gitxPick(title, items, func(m *Model, id string) tea.Cmd {
		i, err := strconv.Atoi(id)
		if err != nil || i < 0 || i >= len(res.Tasks) {
			return nil
		}
		return m.suggestedTaskCard(root, res, i)
	})
	m.picker.SetWidth(100)
	m.picker.SetSize(m.w, m.h)
	note := []string{"Enter shows a task: open it, or let the AI do it.", fmt.Sprintf("%d files read.", res.Files)}
	if res.Truncated {
		note[1] = fmt.Sprintf("%d files read; there may be more (the list stops at %d).", res.Files, len(res.Tasks))
	}
	m.picker.SetNote(note)
	if cursor != "" {
		m.picker.SetCursorID(cursor)
	}
	return nil
}

// suggestedTaskTitle is a row: the kind, padded, then the text.
func suggestedTaskTitle(t suggest.Task) string {
	return fmt.Sprintf("%-5s  %s", t.Kind, t.Text)
}

// suggestedTaskCard is the Cancel-first card of one task. Cancel goes
// back to the list on the same row; Esc only closes the card.
func (m *Model) suggestedTaskCard(root string, res suggest.Result, i int) tea.Cmd {
	t := res.Tasks[i]
	body := t.Where() + "\n\n" + string(t.Kind) + ": " + t.Text +
		"\n\nAsk AI to Do It fills the chat box; nothing is sent until you press Enter."
	m.gitxCard("Suggested Task", body, func(m *Model, id string) tea.Cmd {
		switch id {
		case "open":
			m.openFileAtLine(filepath.Join(root, filepath.FromSlash(t.File)), t.Line)
			return nil
		case "ai":
			return m.gitAIPrefillChat(suggestedTaskPrompt(t))
		}
		return m.showSuggestedTasks(root, res, strconv.Itoa(i))
	},
		confirm.Button{ID: "open", Title: "Open in Editor"},
		confirm.Button{ID: "ai", Title: "Ask AI to Do It"},
	)
	return nil
}

// suggestedTaskPrompt is the chat text for one task.
func suggestedTaskPrompt(t suggest.Task) string {
	var b strings.Builder
	if t.Kind == suggest.KindSlow {
		fmt.Fprintf(&b, "At %s the code may be slow: %s.\n\n", t.Where(), t.Text)
		b.WriteString("Read the code around it first. If it is really a problem, make it faster without changing what it does, and keep the change small. If it is fine as it is, say why and change nothing.")
	} else {
		fmt.Fprintf(&b, "Do this %s at %s: %s\n\n", t.Kind, t.Where(), t.Text)
		b.WriteString("Read the code around it first. Make the smallest change that does it, remove the " + string(t.Kind) + " comment when it is done, and say how I can check the change.")
	}
	return b.String()
}
