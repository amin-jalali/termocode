//go:build unix

package app

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/canvas"
	"github.com/amin-jalali/termocode/internal/workspace/localhost"
)

// CMP-92: the shared plan. The user and the AI both edit one board of
// cards (.termocode/canvas.json): the palette (canvas-open, canvas-add,
// canvas-copy) and the card actions for the user, canvas_read and
// canvas_update for the AI.

func canvasModel(t *testing.T) (*Model, string) {
	t.Helper()
	mm := aiTestModel(t)
	m := &mm
	root := t.TempDir()
	ws, err := localhost.New(root)
	if err != nil {
		t.Fatal(err)
	}
	m.ws = ws
	return m, root
}

func canvasLoad(t *testing.T, root string) canvas.Board {
	t.Helper()
	b, err := canvas.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Spec: Canvas: Add Card… asks for a title and adds an approved card,
// then the list shows it; Enter on it gives its actions; Delete asks
// first with Cancel as the safe, focused answer.
func TestCanvasPaletteFlow(t *testing.T) {
	m, root := canvasModel(t)
	m.dispatchCanvasPalette(canvasAddPaletteID)
	if !m.promptOpen || m.promptKind != promptKindCanvasAdd {
		t.Fatal("canvas-add opened no prompt")
	}
	if _, ok := m.handleAIPromptSubmit("write the parser"); !ok {
		t.Fatal("the add prompt was not routed")
	}
	b := canvasLoad(t, root)
	if len(b.Cards) != 1 || b.Cards[0].Title != "write the parser" || !b.Cards[0].Approved || b.Cards[0].By != canvas.ByYou {
		t.Fatalf("plan after add: %+v", b.Cards)
	}
	if !m.pickerOpen || m.pickerKind != pickerKindCanvas {
		t.Fatal("the list is not shown after the add")
	}
	if box := strings.Join(pickerItemsOf(m.picker), "\n"); !strings.Contains(box, "1. ☐ write the parser") || !strings.Contains(box, "+ Add a card") {
		t.Errorf("list rows:\n%s", box)
	}
	m.handleAIPickerSelect(canvasCardPfx + "c1")
	if !m.pickerOpen || m.pickerKind != pickerKindCanvasCard || m.canvasSel != "c1" {
		t.Fatal("Enter on a card opened no actions")
	}
	m.handleAIPickerSelect(canvasActPfx + "status:" + canvas.StatusDoing)
	if b := canvasLoad(t, root); b.Cards[0].Status != canvas.StatusDoing {
		t.Errorf("Mark doing: %+v", b.Cards[0])
	}
	m.handleAIPickerSelect(canvasCardPfx + "c1")
	m.handleAIPickerSelect(canvasActPfx + "delete")
	if !m.confirmOpen || m.confirmKind != confirmKindCanvasDelete || m.confirm.SafeID() != "cancel" {
		t.Fatal("Delete did not ask first with Cancel as the safe answer")
	}
	m.confirmOpen = false
	m.handleAIConfirm("cancel")
	if len(canvasLoad(t, root).Cards) != 1 {
		t.Fatal("Cancel deleted the card")
	}
	m.handleAIPickerSelect(canvasCardPfx + "c1")
	m.handleAIPickerSelect(canvasActPfx + "delete")
	m.confirmOpen = false
	m.handleAIConfirm("delete")
	if len(canvasLoad(t, root).Cards) != 0 {
		t.Fatal("Delete kept the card")
	}
	if m.dispatchCanvasPalette(canvasOpenPaletteID); !m.pickerOpen || m.pickerKind != pickerKindCanvas {
		t.Error("canvas-open shows no list")
	}
	if _, ok := m.dispatchCanvasPalette(canvasCopyPaletteID); !ok {
		t.Error("canvas-copy is not a palette command")
	}
	for _, id := range []string{canvasOpenPaletteID, canvasAddPaletteID, canvasCopyPaletteID} {
		if p, ok := aiActionPolicies[id]; !ok || p.Gate != aiGateNever {
			t.Errorf("%s: AI policy %+v, want never (the AI has canvas_read / canvas_update)", id, p)
		}
	}
}

// Spec: the AI adds cards that wait for the user's OK, updates status,
// cannot approve, and a refused change saves nothing; canvas_read shows
// the plan.
func TestCanvasAITools(t *testing.T) {
	m, root := canvasModel(t)
	tools := aiCanvasTools(root, direct(m))
	upd, ok := ai.FindTool(tools, "canvas_update")
	if !ok {
		t.Fatal("no canvas_update")
	}
	read, ok := ai.FindTool(tools, "canvas_read")
	if !ok || !read.ReadOnly {
		t.Fatal("no read-only canvas_read")
	}
	ctx := context.Background()
	out, err := upd.Run(ctx, map[string]any{"changes": []any{
		map[string]any{"op": "add", "title": "split the lexer", "files": []any{"internal/sql/lex.go"}},
		map[string]any{"op": "add", "title": "add golden tests", "status": "doing"},
	}})
	if err != nil || !strings.Contains(out, "added c1") || !strings.Contains(out, "added c2") {
		t.Fatalf("update: %q %v", out, err)
	}
	b := canvasLoad(t, root)
	if len(b.Cards) != 2 || b.Cards[0].Approved || b.Cards[0].By != canvas.ByAI || b.Cards[1].Status != canvas.StatusDoing {
		t.Fatalf("AI cards: %+v", b.Cards)
	}
	if !toastHas(m, "The AI changed the shared plan") {
		t.Error("no toast after the AI's change")
	}
	if err := upd.Validate(map[string]any{"changes": []any{map[string]any{"op": "approve", "id": "c1"}}}); err == nil {
		t.Error("the AI may approve a card")
	}
	_, err = upd.Run(ctx, map[string]any{"changes": []any{
		map[string]any{"op": "status", "id": "c1", "status": "done"},
		map[string]any{"op": "remove", "id": "c9"},
	}})
	if err == nil || !strings.Contains(err.Error(), "nothing was saved") {
		t.Fatalf("a bad change: %v", err)
	}
	if canvasLoad(t, root).Cards[0].Status != canvas.StatusTodo {
		t.Error("a refused call saved its first change")
	}
	txt, err := read.Run(ctx, map[string]any{})
	if err != nil || !strings.Contains(txt, "1. c1 [to do] split the lexer (by AI, needs the user's OK)") || !strings.Contains(txt, "files: internal/sql/lex.go") {
		t.Errorf("canvas_read: %q %v", txt, err)
	}
	var names []string
	for _, tl := range m.aiChatTools(root, direct(m)) {
		names = append(names, tl.Name)
	}
	if !slices.Contains(names, "canvas_read") || !slices.Contains(names, "canvas_update") {
		t.Error("the chat has no canvas tools")
	}
}
