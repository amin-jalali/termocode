package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/canvas"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The shared plan's AI tools (CMP-92). canvas_read returns the plan;
// canvas_update applies a list of changes as the AI. Both run on the UI
// goroutine (call), so they never race the user's own edits, and need no
// card: the AI only writes the plan file, cannot approve a card and
// cannot remove a card the user wrote or approved (canvas.Board.Apply).
// A change tells the user with a toast.

// canvasMaxOps is the most changes one canvas_update call may make.
const canvasMaxOps = 30

// aiCanvasTools are canvas_read and canvas_update for root.
func aiCanvasTools(root string, call aiIDECaller) []ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	read := ai.Tool{
		Name: "canvas_read",
		Description: "Read the shared plan of this workspace: the cards the user and you keep together, in order, " +
			"with id, status (to do, doing, done), who wrote each, whether the user approved it, its note and files.",
		Params:   map[string]any{},
		ReadOnly: true,
		Describe: func(map[string]any) string { return "Read the shared plan" },
		Run: func(ctx context.Context, _ map[string]any) (string, error) {
			return call(ctx, func(m *Model) (string, tea.Cmd, error) {
				b, err := canvas.Load(root)
				if err != nil {
					return "", nil, err
				}
				return b.Text(), nil, nil
			})
		},
	}
	update := ai.Tool{
		Name: "canvas_update",
		Description: "Change the shared plan of this workspace (the user sees it in Canvas: Open Shared Plan). " +
			"Use it to propose steps (add), keep the plan current while you work (status doing / done), " +
			"refine a card (edit) or reorder (move). A card you add waits for the user's OK; editing an approved card's " +
			"title or note takes its OK away. You cannot approve cards, and you can only remove cards you added that the user " +
			"has not approved. Read the plan with canvas_read first.",
		Params: map[string]any{"changes": map[string]any{
			"type":        "array",
			"description": "the changes, applied in order (at most 30)",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"op":     map[string]any{"type": "string", "enum": canvas.OpKinds},
					"id":     str("the card id (c1, c2 …) for every op but add"),
					"title":  str("add: the card's title (one short line); edit: a new title"),
					"note":   str("add / edit: a longer note (edit: an empty string clears it)"),
					"status": map[string]any{"type": "string", "enum": canvas.Statuses, "description": "add (default todo) / status"},
					"to":     map[string]any{"type": "integer", "description": "move: the new place, 1 is the top"},
					"files": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
						"description": "add / edit: workspace-relative paths the card is about"},
				},
				"required": []string{"op"},
			},
		}},
		Required: []string{"changes"},
		Describe: func(a map[string]any) string {
			ops, err := canvasParseOps(a)
			if err != nil {
				return "Change the shared plan"
			}
			return fmt.Sprintf("Change the shared plan (%d change%s)", len(ops), plural(len(ops)))
		},
		Validate: func(a map[string]any) error { _, err := canvasParseOps(a); return err },
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			ops, err := canvasParseOps(a)
			if err != nil {
				return "", err
			}
			return call(ctx, func(m *Model) (string, tea.Cmd, error) {
				return m.canvasAIUpdate(root, ops)
			})
		},
	}
	return []ai.Tool{read, update}
}

// canvasAIUpdate applies ops as the AI: all or nothing (a change that
// fails saves none of them).
func (m *Model) canvasAIUpdate(root string, ops []canvas.Op) (string, tea.Cmd, error) {
	b, err := canvas.Load(root)
	if err != nil {
		return "", nil, err
	}
	now := clock.Now()
	var did []string
	for n, op := range ops {
		_, what, err := b.Apply(op, canvas.ByAI, now, canvasRedact)
		if err != nil {
			return "", nil, fmt.Errorf("change %d (%s): %w; nothing was saved", n+1, op.Kind, err)
		}
		did = append(did, what)
	}
	if err := canvas.Save(root, &b); err != nil {
		return "", nil, err
	}
	sum := strings.Join(did, "; ")
	cmd := m.pushToastDetail(toast.Info, "The AI changed the shared plan", aiCardText(sum, 200)+"\nCanvas: Open Shared Plan shows it.")
	if m.pickerOpen && (m.pickerKind == pickerKindCanvas) {
		cmd = tea.Batch(cmd, m.openCanvas("")) // the open list shows the new plan
	}
	return "Saved: " + sum + ".\n\n" + b.Text(), cmd, nil
}

// canvasParseOps reads canvas_update's "changes".
func canvasParseOps(a map[string]any) ([]canvas.Op, error) {
	raw, ok := a["changes"].([]any)
	if !ok {
		return nil, errors.New(`"changes" must be a list of {"op", ...}`)
	}
	if len(raw) == 0 {
		return nil, errors.New(`"changes" is empty`)
	}
	if len(raw) > canvasMaxOps {
		return nil, fmt.Errorf("at most %d changes in one call", canvasMaxOps)
	}
	out := make([]canvas.Op, 0, len(raw))
	for i, r := range raw {
		o, ok := r.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("change %d is not an object", i+1)
		}
		op := canvas.Op{Kind: strings.TrimSpace(ai.ArgString(o, "op")), ID: strings.TrimSpace(ai.ArgString(o, "id")),
			Title: ai.ArgString(o, "title"), Note: ai.ArgString(o, "note"), Status: strings.TrimSpace(ai.ArgString(o, "status")),
			To: ai.ArgInt(o, "to")}
		_, op.SetNote = o["note"]
		if fs, ok := o["files"].([]any); ok {
			op.SetFiles = true
			for _, f := range fs {
				if s, ok := f.(string); ok {
					op.Files = append(op.Files, s)
				}
			}
		}
		switch op.Kind {
		case canvas.OpAdd:
			if strings.TrimSpace(op.Title) == "" {
				return nil, fmt.Errorf("change %d: add needs a title", i+1)
			}
		case canvas.OpEdit, canvas.OpStatus, canvas.OpMove, canvas.OpRemove:
			if op.ID == "" {
				return nil, fmt.Errorf("change %d: %s needs the card id", i+1, op.Kind)
			}
			if op.Kind == canvas.OpMove && op.To < 1 {
				return nil, fmt.Errorf("change %d: move needs \"to\" (1 is the top)", i+1)
			}
		case canvas.OpApprove, canvas.OpUnapprove:
			return nil, fmt.Errorf("change %d: only the user approves cards", i+1)
		default:
			return nil, fmt.Errorf("change %d: op must be one of %s", i+1, strings.Join(canvas.OpKinds, ", "))
		}
		out = append(out, op)
	}
	return out, nil
}
