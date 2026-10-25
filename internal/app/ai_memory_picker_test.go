package app

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/aimemory"
	"github.com/amin-jalali/termocode/internal/picker"
)

// OM-2: AI: Memory… lists the lessons (newest first) to view, open in
// the editor and forget.

func aiMemoryPickerModel(t *testing.T) (*Model, string, []aimemory.Lesson) {
	t.Helper()
	m, _, dir := aiRulesModel(t, "")
	ls := []aimemory.Lesson{
		aiTestLesson("2026-10-23", "Docs build", "Run make docs.", "Makefile"),
		aiTestLesson("2026-10-24", "Use -race", "CI runs the tests with -race."),
		aiTestLesson("2026-10-25", "a.go is generated", "Edit gen.go instead.", "a.go"),
	}
	seedAILessons(t, dir, ls...)
	setAIMemory(aiMemoryOn)
	return m, dir, ls
}

func (m *Model) aiMemoryConfirmForTest(id string) tea.Cmd {
	m.confirmOpen = false
	cmd, _ := m.handleAIConfirm(id)
	return cmd
}

func pickerItemsOf(p picker.Model) []string {
	return strings.Split(ansi.Strip(p.Box()), "\n")
}

// Spec: the rows are the lessons newest first, then Forget all…; the AI
// can never open the picker.
func TestAIMemoryPickerRows(t *testing.T) {
	m, _, _ := aiMemoryPickerModel(t)
	m.dispatchAIPalette("ai-memory")
	if !m.pickerOpen || m.pickerKind != pickerKindAIMemory {
		t.Fatal("no memory picker")
	}
	box := strings.Join(pickerItemsOf(m.picker), "\n")
	i, j, k, f := strings.Index(box, "2026-10-25 · a.go is generated"), strings.Index(box, "2026-10-24 · Use -race"), strings.Index(box, "2026-10-23 · Docs build"), strings.Index(box, "Forget all…")
	if i < 0 || j < i || k < j || f < k {
		t.Fatalf("rows out of order (%d %d %d %d):\n%s", i, j, k, f, box)
	}
	if p, ok := aiActionPolicies["ai-memory"]; !ok || p.Gate != aiGateNever {
		t.Errorf("ai-memory policy %+v", p)
	}
	// No lessons: a toast instead, naming the switch while memory is off.
	m2, _, _ := aiRulesModel(t, "")
	m2.dispatchAIPalette("ai-memory")
	if m2.pickerOpen || !strings.Contains(aiToastText(m2), "No lessons") || !strings.Contains(aiToastText(m2), "AI: Memory: Turn On/Off") {
		t.Errorf("empty: picker %v toast %q", m2.pickerOpen, aiToastText(m2))
	}
	// The "memory: N lessons saved · view" line opens the picker too.
	m3, _, _ := aiMemoryPickerModel(t)
	m3.onAIButton("memory-view")
	if !m3.pickerOpen || m3.pickerKind != pickerKindAIMemory {
		t.Error("the view button does not open the picker")
	}
}

// Spec: picking a lesson shows it (Close first); Forget… asks with Keep
// first and focused; Forget removes exactly that section; Keep and Esc
// keep it.
func TestAIMemoryForgetCard(t *testing.T) {
	m, dir, ls := aiMemoryPickerModel(t)
	m.dispatchAIPalette("ai-memory")
	m.pickerOpen = false
	m.handleAIPickerSelect(ls[1].ID)
	if !m.confirmOpen || m.confirmKind != confirmKindAIMemoryLesson {
		t.Fatal("no lesson card")
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"Use -race", "CI runs the tests with -race.", "2026-10-24", "Close", "Open in editor", "Forget…"} {
		if !strings.Contains(box, want) {
			t.Errorf("lesson card lacks %q:\n%s", want, box)
		}
	}
	m.aiMemoryConfirmForTest("forget")
	if !m.confirmOpen || m.confirmKind != confirmKindAIMemoryForget || m.confirm.SafeID() != "keep" {
		t.Fatalf("forget card: open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	if !strings.Contains(ansi.Strip(m.confirm.Box()), "Forget this lesson?") {
		t.Errorf("forget card:\n%s", ansi.Strip(m.confirm.Box()))
	}
	before, _ := os.ReadFile(aimemory.Path(dir))
	m.aiMemoryConfirmForTest("keep")
	if after, _ := os.ReadFile(aimemory.Path(dir)); string(after) != string(before) {
		t.Fatal("Keep changed the file")
	}
	// Esc keeps too.
	m.handleAIPickerSelect(ls[1].ID)
	m.aiMemoryConfirmForTest("forget")
	m.confirmOpen = false
	m.onAIConfirmClosed()
	if after, _ := os.ReadFile(aimemory.Path(dir)); string(after) != string(before) {
		t.Fatal("Esc changed the file")
	}
	m.handleAIPickerSelect(ls[1].ID)
	m.aiMemoryConfirmForTest("forget")
	m.aiMemoryConfirmForTest("forget")
	got, _ := aimemory.Load(dir)
	if len(got) != 2 || got[0].ID != ls[0].ID || got[1].ID != ls[2].ID {
		t.Fatalf("after forget: %+v", got)
	}
	// Forget all… asks the same way and empties the file.
	m.handleAIPickerSelect(aiMemoryForgetAllID)
	if !m.confirmOpen || m.confirmKind != confirmKindAIMemoryForgetAll || m.confirm.SafeID() != "keep" {
		t.Fatal("no forget-all card")
	}
	m.aiMemoryConfirmForTest("forget")
	if got, _ := aimemory.Load(dir); len(got) != 0 {
		t.Fatalf("after forget all: %+v", got)
	}
}

// Spec: Open in editor opens lessons.md at the lesson's section.
func TestAIMemoryEditOpensFile(t *testing.T) {
	m, dir, ls := aiMemoryPickerModel(t)
	c := aiTestNvim(t, m)
	m.dispatchAIPalette("ai-memory")
	m.pickerOpen = false
	m.handleAIPickerSelect(ls[2].ID)
	m.aiMemoryConfirmForTest("open")
	name, _ := c.EvalLuaString(`return vim.api.nvim_buf_get_name(0)`)
	if !strings.HasSuffix(name, "lessons.md") || !strings.HasPrefix(name, dir) {
		t.Fatalf("current buffer %q", name)
	}
	line, _ := c.EvalLuaString(`return vim.api.nvim_get_current_line()`)
	if !strings.Contains(line, "{#"+ls[2].ID+"}") {
		t.Errorf("cursor line %q, want the lesson's heading", line)
	}
}
