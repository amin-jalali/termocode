package ai

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/agentdag"
)

// The chat's todo list (OMX-7): the AI keeps it with todo_write /
// todo_read; it is saved with the chat and sent in the system prompt
// every turn (so a summary of old turns never loses it).

// Todo states.
const (
	TodoPending = "pending"
	TodoActive  = "in_progress"
	TodoDone    = "done"
)

// MaxTodos and TodoTextMax cap the list and one item's text (runes).
const (
	MaxTodos    = 30
	TodoTextMax = 200
)

// Todo is one item of a chat's todo list.
type Todo struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

// ParseTodos reads todo_write's "todos" argument: a list of {text,
// status}. Texts are cleaned to one line and capped; a missing status
// is pending.
func ParseTodos(args map[string]any) ([]Todo, error) {
	raw, ok := args["todos"].([]any)
	if !ok {
		return nil, errors.New(`"todos" must be a list of {"text", "status"}`)
	}
	if len(raw) > MaxTodos {
		return nil, fmt.Errorf("at most %d todos (got %d); merge small ones", MaxTodos, len(raw))
	}
	out := make([]Todo, 0, len(raw))
	for i, r := range raw {
		obj, _ := r.(map[string]any)
		text := agentdag.Clean(ArgString(obj, "text"))
		if text == "" {
			return nil, fmt.Errorf("todo %d has no text", i+1)
		}
		if rs := []rune(text); len(rs) > TodoTextMax {
			text = string(rs[:TodoTextMax-1]) + "…"
		}
		st := strings.TrimSpace(ArgString(obj, "status"))
		switch st {
		case "":
			st = TodoPending
		case TodoPending, TodoActive, TodoDone:
		default:
			return nil, fmt.Errorf("todo %d: status must be pending, in_progress or done (got %q)", i+1, st)
		}
		out = append(out, Todo{Text: text, Status: st})
	}
	return out, nil
}

// TodosOpen is how many todos are not done.
func TodosOpen(ts []Todo) int {
	n := 0
	for _, t := range ts {
		if t.Status != TodoDone {
			n++
		}
	}
	return n
}

// TodosText is the list as the AI reads it: "[x] done", "[~] in
// progress", "[ ] pending", one a line.
func TodosText(ts []Todo) string {
	lines := make([]string, len(ts))
	for i, t := range ts {
		mark := "[ ]"
		switch t.Status {
		case TodoDone:
			mark = "[x]"
		case TodoActive:
			mark = "[~]"
		}
		lines[i] = mark + " " + t.Text
	}
	return strings.Join(lines, "\n")
}

// TodosPrompt is the system prompt block of a list with open todos (""
// when nothing is open).
func TodosPrompt(ts []Todo) string {
	open := TodosOpen(ts)
	if open == 0 {
		return ""
	}
	return "\n\nYour todo list for this chat (" + strconv.Itoa(open) + " open; keep it current with todo_write):\n" + TodosText(ts)
}
