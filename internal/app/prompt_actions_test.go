package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/promptactions"
)

func paTestModel(t *testing.T) *Model {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	return &Model{w: 120, h: 40}
}

func paSaved(t *testing.T) []promptactions.Action {
	t.Helper()
	p, err := promptActionsPath()
	if err != nil {
		t.Fatal(err)
	}
	list, _, err := promptactions.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// New: name ("Group/Name"), then the prompt (seeded with the chat input),
// then where it runs; the action is saved in prompt-actions.json.
func TestPromptActionNewFlowSaves(t *testing.T) {
	m := paTestModel(t)
	m.aiS().input = []rune("Review my diff.")
	if _, ok := m.dispatchPromptActionsPalette(paNewPaletteID); !ok {
		t.Fatal("new not handled")
	}
	if !m.promptOpen || m.promptKind != promptKindPromptActionName {
		t.Fatalf("name prompt not open: %v %v", m.promptOpen, m.promptKind)
	}
	m.promptOpen = false
	m.onPromptActionName("Review/Diff")
	if !m.promptOpen || m.promptKind != promptKindPromptActionBody || m.prompt.Value() != "Review my diff." {
		t.Fatalf("body prompt: %v %v %q", m.promptOpen, m.promptKind, m.prompt.Value())
	}
	m.promptOpen = false
	m.onPromptActionBody(`Look at it.\nThen say what is wrong.`)
	if !m.pickerOpen || m.pickerKind != pickerKindPromptActionTarget {
		t.Fatalf("target picker: %v %v", m.pickerOpen, m.pickerKind)
	}
	m.pickerOpen = false
	m.onPromptActionTarget(promptactions.TargetTerminal)
	list := paSaved(t)
	if len(list) != 1 || list[0].Group != "Review" || list[0].Name != "Diff" ||
		list[0].Prompt != "Look at it.\nThen say what is wrong." || list[0].Target != promptactions.TargetTerminal {
		t.Fatalf("saved %+v", list)
	}
	// A bad name says why and saves nothing more.
	m.onPromptActionName("Group/")
	if m.promptOpen && m.promptKind == promptKindPromptActionBody {
		t.Error("bad name went on")
	}
	if !strings.Contains(wtLastToast(m), "name") {
		t.Errorf("toast %q", wtLastToast(m))
	}
}

// The picker lists each action as "Group · Name" with where it runs,
// and the rows to make, import, export and edit them.
func TestPromptActionPickerItems(t *testing.T) {
	items := promptActionPickerItems([]promptactions.Action{
		{Name: "Diff", Group: "Review", Prompt: "Review my diff.\nmore"},
		{Name: "Tests", Prompt: "Write tests.", Target: promptactions.TargetTerminal},
	})
	var titles, ids []string
	for _, it := range items {
		titles = append(titles, it.Title+" | "+it.Hint)
		ids = append(ids, it.ID)
	}
	all := strings.Join(titles, "\n")
	for _, want := range []string{"Tests | terminal · Write tests.", "Review · Diff | chat · Review my diff.", "New Prompt Action", "Import", "Export", "prompt-actions.json"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in\n%s", want, all)
		}
	}
	if !strings.HasPrefix(ids[0], paRunPrefix) {
		t.Errorf("actions should come first: %v", ids)
	}
}

// Picking a chat action sends its prompt to the chat; a terminal action
// with no terminal tab says so and types nothing.
func TestPromptActionRun(t *testing.T) {
	m := paTestModel(t)
	p, _ := promptActionsPath()
	if err := promptactions.Save(p, []promptactions.Action{
		{Name: "Diff", Group: "Review", Prompt: "Review my diff."},
		{Name: "Tests", Prompt: "Write tests.", Target: promptactions.TargetTerminal},
	}); err != nil {
		t.Fatal(err)
	}
	var sent string
	old := promptActionSendChat
	promptActionSendChat = func(_ *Model, text string) tea.Cmd { sent = text; return nil }
	t.Cleanup(func() { promptActionSendChat = old })
	m.onPromptActionPicked(paRunPrefix + "review/diff")
	if sent != "Review my diff." {
		t.Errorf("chat got %q", sent)
	}
	m.onPromptActionPicked(paRunPrefix + "tests")
	if !strings.Contains(wtLastToast(m), "terminal tab") {
		t.Errorf("toast %q", wtLastToast(m))
	}
	m.onPromptActionPicked(paRunPrefix + "gone")
	if !strings.Contains(wtLastToast(m), "not there") {
		t.Errorf("toast %q", wtLastToast(m))
	}
}

// Import shows a Cancel-first card with what it adds and replaces; only
// Import writes. Esc is Cancel.
func TestPromptActionImportAsksFirst(t *testing.T) {
	m := paTestModel(t)
	p, _ := promptActionsPath()
	if err := promptactions.Save(p, []promptactions.Action{{Name: "Diff", Group: "Review", Prompt: "old"}}); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "in.json")
	if err := promptactions.Save(src, []promptactions.Action{{Name: "diff", Group: "review", Prompt: "new"}, {Name: "Tests", Prompt: "Write tests."}}); err != nil {
		t.Fatal(err)
	}
	m.onPromptActionImportPath(src)
	if !m.confirmOpen || m.confirmKind != confirmKindPromptActionsImport {
		t.Fatalf("no card: %v %v", m.confirmOpen, m.confirmKind)
	}
	if b := m.confirm.Buttons(); len(b) < 2 || b[0].ID != "cancel" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("buttons %+v", b)
	}
	m.confirmOpen = false
	m.answerPromptActionsImport("") // Esc
	if list := paSaved(t); len(list) != 1 || list[0].Prompt != "old" {
		t.Fatalf("Esc wrote: %+v", list)
	}
	m.onPromptActionImportPath(src)
	m.confirmOpen = false
	m.answerPromptActionsImport("import")
	list := paSaved(t)
	if len(list) != 2 {
		t.Fatalf("after import: %+v", list)
	}
	for _, a := range list {
		if a.Key() == "review/diff" && a.Prompt != "new" {
			t.Errorf("not replaced: %+v", a)
		}
	}
	// A broken file only says why.
	bad := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(bad, []byte("{broken"), 0o644)
	m.onPromptActionImportPath(bad)
	if m.confirmOpen {
		t.Error("card for a broken file")
	}
}

// Export writes the actions to a file; over an existing file it asks
// first (Cancel first) and only Replace writes.
func TestPromptActionExport(t *testing.T) {
	m := paTestModel(t)
	p, _ := promptActionsPath()
	if err := promptactions.Save(p, []promptactions.Action{{Name: "Diff", Prompt: "x"}}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.json")
	m.onPromptActionExportPath(out)
	if got, _, err := promptactions.Load(out); err != nil || len(got) != 1 {
		t.Fatalf("export: %+v %v", got, err)
	}
	if err := os.WriteFile(out, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.onPromptActionExportPath(out)
	if !m.confirmOpen || m.confirmKind != confirmKindPromptActionsExport || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no replace card: %v %v", m.confirmOpen, m.confirmKind)
	}
	m.confirmOpen = false
	m.answerPromptActionsExport("")
	if b, _ := os.ReadFile(out); string(b) != "keep" {
		t.Errorf("Esc overwrote: %q", b)
	}
	m.onPromptActionExportPath(out)
	m.confirmOpen = false
	m.answerPromptActionsExport("replace")
	if got, _, err := promptactions.Load(out); err != nil || len(got) != 1 {
		t.Errorf("replace: %+v %v", got, err)
	}
}

// Every palette row is in the palette and the AI never opens these
// (they are pickers and prompts only the user answers).
func TestPromptActionPaletteAndPolicies(t *testing.T) {
	ids := map[string]bool{}
	for _, it := range promptActionsPaletteItems() {
		ids[it.ID] = true
		if p, ok := aiActionPolicyFor(it.ID); !ok || p.Gate != aiGateNever {
			t.Errorf("%s policy %+v", it.ID, p)
		}
	}
	for _, id := range []string{paRunPaletteID, paNewPaletteID, paImportPaletteID, paExportPaletteID, paEditPaletteID} {
		if !ids[id] {
			t.Errorf("%s not in the palette", id)
		}
	}
}
