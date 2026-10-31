package app

import (
	"context"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// The AI's todo list (OMX-7). todo_write replaces the chat's list,
// todo_read returns it. Both only change the chat, never the workspace,
// so they run without a card (gate auto); the list is shown as a card at
// the end of the chat and sent in every turn's system prompt.

// aiTodoTools are the chat's todo tools; call runs on the UI goroutine.
func aiTodoTools(call aiIDECaller) []ai.Tool {
	write := ai.Tool{
		Name: "todo_write",
		Description: "Write the todo list of this chat (the whole list each time; it replaces the old one). " +
			"Use it for work with three or more steps: one item per step, mark the one you work on in_progress " +
			"and mark items done as soon as they are done. The user sees the list. An empty list clears it.",
		Params: map[string]any{"todos": map[string]any{
			"type":        "array",
			"description": "every todo, in order",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text":   map[string]any{"type": "string", "description": "what to do, one short line"},
					"status": map[string]any{"type": "string", "enum": []string{ai.TodoPending, ai.TodoActive, ai.TodoDone}},
				},
				"required": []string{"text", "status"},
			},
		}},
		Required: []string{"todos"},
		Describe: func(a map[string]any) string {
			ts, err := ai.ParseTodos(a)
			if err != nil {
				return "Update todos"
			}
			return "Update todos (" + aiTodoCount(ts) + ")"
		},
		Validate: func(a map[string]any) error { _, err := ai.ParseTodos(a); return err },
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			ts, err := ai.ParseTodos(a)
			if err != nil {
				return "", err
			}
			return call(ctx, func(m *Model) (string, tea.Cmd, error) {
				m.aiS().conv.Todos = ts
				if len(ts) == 0 {
					return "todo list cleared", nil, nil
				}
				return "todo list saved (" + aiTodoCount(ts) + ")", nil, nil
			})
		},
	}
	read := ai.Tool{
		Name:        "todo_read",
		Description: "Read the todo list of this chat: [x] done, [~] in progress, [ ] pending.",
		Params:      map[string]any{},
		Describe:    func(map[string]any) string { return "Read todos" },
		Run: func(ctx context.Context, _ map[string]any) (string, error) {
			return call(ctx, func(m *Model) (string, tea.Cmd, error) {
				ts := m.aiS().conv.Todos
				if len(ts) == 0 {
					return "the todo list is empty", nil, nil
				}
				return ai.TodosText(ts), nil, nil
			})
		},
	}
	return []ai.Tool{write, read}
}

// aiTodoCount is "1 of 3 done".
func aiTodoCount(ts []ai.Todo) string {
	return strconv.Itoa(len(ts)-ai.TodosOpen(ts)) + " of " + strconv.Itoa(len(ts)) + " done"
}

// aiTodoCard is the todo list card at the end of the chat (nil when the
// list is empty).
func (m Model) aiTodoCard(width int) []aiLine {
	if m.ai == nil || len(m.ai.conv.Todos) == 0 {
		return nil
	}
	ts := m.ai.conv.Todos
	ind := strings.Repeat(" ", aiIndent)
	lim := max(width-aiIndent-3, 4)
	cut := func(t string) string {
		if runewidth.StringWidth(t) > lim {
			return runewidth.Truncate(t, lim, "…")
		}
		return t
	}
	head := "Todos · " + aiTodoCount(ts) + m.aiKeepGoingLabel()
	out := []aiLine{{}, {text: ind + cut(head), style: aiStyle(theme.Fg(theme.AIAccent).Bold(true))}}
	for _, t := range ts {
		mark, st := "☐ ", theme.Fg(theme.TextPrimary)
		switch t.Status {
		case ai.TodoDone:
			mark, st = "☑ ", theme.Fg(theme.TextDim)
		case ai.TodoActive:
			mark, st = "◐ ", theme.Fg(theme.AIAccent)
		}
		out = append(out, aiLine{text: ind + "  " + cut(mark+t.Text), style: aiStyle(st)})
	}
	return out
}
