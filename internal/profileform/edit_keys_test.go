package profileform

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

func kt(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func send(m Model, ks ...tea.KeyMsg) (Model, []tea.Msg) {
	var got []tea.Msg
	for _, k := range ks {
		var cmd tea.Cmd
		m, cmd = m.Update(k)
		if cmd != nil {
			got = append(got, cmd())
		}
	}
	return m, got
}

// editing is the review form with the focus editing it.
func editing(t *testing.T, it item) Model {
	t.Helper()
	m := New("my-review", review(), cfg(2))
	m.SetSize(120, 40)
	m = focusOn(m, it)
	m, _ = send(m, kt(tea.KeyEnter))
	if !m.editing {
		t.Fatalf("Enter on %v did not start editing", it)
	}
	return m
}

func TestEditingKeysInATextField(t *testing.T) {
	m := editing(t, item{kind: iTitle})
	*m.text(m.foc) = "hello"
	m.caret = 5
	m, _ = send(m, kt(tea.KeyLeft), kt(tea.KeyLeft), kt(tea.KeyBackspace))
	if m.title != "helo" || m.caret != 2 {
		t.Fatalf("Left Left Backspace: %q caret %d", m.title, m.caret)
	}
	m, _ = send(m, kt(tea.KeyDelete))
	if m.title != "heo" {
		t.Errorf("Delete: %q", m.title)
	}
	m, _ = send(m, kt(tea.KeyHome), kt(tea.KeyBackspace))
	if m.caret != 0 || m.title != "heo" {
		t.Errorf("Backspace at the start: %q caret %d", m.title, m.caret)
	}
	m, _ = send(m, kt(tea.KeyEnd), kt(tea.KeyDelete), kt(tea.KeyRight))
	if m.caret != 3 || m.title != "heo" {
		t.Errorf("Delete/Right at the end: %q caret %d", m.title, m.caret)
	}
	m, _ = send(m, kt(tea.KeyCtrlA), kt(tea.KeySpace), kt(tea.KeyCtrlE))
	if m.title != " heo" || m.caret != 4 {
		t.Errorf("Ctrl+A Space Ctrl+E: %q caret %d", m.title, m.caret)
	}
	// Pasted line breaks and tabs become spaces in a one-line field;
	// control bytes are dropped.
	m, _ = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\nb\tc\x07")})
	if m.title != " heoa b  c" {
		t.Errorf("paste into a one-line field: %q", m.title)
	}
	// Up and Down do nothing in a one-line field.
	before := m.caret
	m, _ = send(m, kt(tea.KeyUp), kt(tea.KeyDown))
	if m.caret != before {
		t.Errorf("Up/Down moved the caret in a one-line field")
	}
	// Enter ends editing in a one-line field.
	m, _ = send(m, kt(tea.KeyEnter))
	if m.editing {
		t.Error("Enter did not end editing")
	}
}

func TestEditingEndsWithEscOrMovesWithTab(t *testing.T) {
	m := editing(t, item{kind: iTitle})
	m, msgs := send(m, kt(tea.KeyEsc))
	if m.editing || len(msgs) != 0 {
		t.Errorf("Esc while editing: editing %v, msgs %v (it must not close the form)", m.editing, msgs)
	}
	m = editing(t, item{kind: iTitle})
	m, _ = send(m, kt(tea.KeyTab))
	if m.editing || m.foc.kind != iBudget {
		t.Errorf("Tab while editing: focus %v editing %v", m.foc, m.editing)
	}
	m = editing(t, item{kind: iTitle})
	m, _ = send(m, kt(tea.KeyShiftTab))
	if m.foc.kind != iName {
		t.Errorf("Shift+Tab while editing: focus %v", m.foc)
	}
}

func TestEditingThePromptMovesBetweenLines(t *testing.T) {
	m := editing(t, item{iSPrompt, 0})
	*m.text(m.foc) = "abcd\nef\nghij"
	m.caret = 11 // "ghij", column 2
	m, _ = send(m, kt(tea.KeyUp))
	if m.caret != 7 { // "ef" is short: its end
		t.Errorf("Up: caret %d, want 7", m.caret)
	}
	m, _ = send(m, kt(tea.KeyUp))
	if m.caret != 2 {
		t.Errorf("Up again: caret %d, want 2", m.caret)
	}
	m, _ = send(m, kt(tea.KeyUp))
	if m.caret != 2 {
		t.Errorf("Up on the first line: caret %d, want 2", m.caret)
	}
	m, _ = send(m, kt(tea.KeyDown), kt(tea.KeyDown))
	if m.caret != 10 {
		t.Errorf("Down Down: caret %d, want 10", m.caret)
	}
	m, _ = send(m, kt(tea.KeyDown))
	if m.caret != 10 {
		t.Errorf("Down on the last line: caret %d", m.caret)
	}
	m, _ = send(m, kt(tea.KeyHome))
	if m.caret != 8 {
		t.Errorf("Home: caret %d, want 8", m.caret)
	}
	m, _ = send(m, kt(tea.KeyEnter), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x\r\ny")})
	if got := m.steps[0].system; got != "abcd\nef\n\nx\n\nyghij" {
		t.Errorf("Enter and a pasted CRLF in the prompt: %q", got)
	}
}

func TestEditingAFieldThatIsGoneStopsEditing(t *testing.T) {
	m := editing(t, item{iSTitle, 0})
	m.foc = item{iSTitle, 99}
	m, _ = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if m.editing {
		t.Error("typing into a step that is gone kept editing")
	}
	m.foc = item{kind: iSave}
	if m.text(m.foc) != nil {
		t.Error("a button has text")
	}
}

func TestNavLettersAndArrowsOnButtons(t *testing.T) {
	m := New("my-review", review(), cfg(1))
	m.SetSize(120, 40)
	m = focusOn(m, item{kind: iName})
	m, _ = keys(m, "j")
	if m.foc.kind != iTitle {
		t.Errorf("j: focus %v", m.foc)
	}
	m, _ = keys(m, "k")
	if m.foc.kind != iName {
		t.Errorf("k: focus %v", m.foc)
	}
	m, msgs := keys(m, "?")
	if len(msgs) != 1 {
		t.Fatalf("?: %v", msgs)
	}
	if _, ok := msgs[0].(KeysMsg); !ok {
		t.Errorf("? sent %T", msgs[0])
	}
	m, msgs = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Paste: true})
	if len(msgs) != 0 {
		t.Errorf("a paste outside a field did %v", msgs)
	}
	m = focusOn(m, item{kind: iSave})
	m, _ = send(m, kt(tea.KeyRight))
	if m.foc.kind != iCancel {
		t.Errorf("Right on Save: focus %v", m.foc)
	}
	m, _ = send(m, kt(tea.KeyLeft))
	if m.foc.kind != iSave {
		t.Errorf("Left on Cancel: focus %v", m.foc)
	}
	m = focusOn(m, item{kind: iName})
	m, _ = send(m, kt(tea.KeyRight))
	if m.foc.kind != iName || m.name != "my-review" {
		t.Errorf("Right on a text field changed %v %q", m.foc, m.name)
	}
	if _, cmd := m.Update(tea.MouseMsg{}); cmd != nil {
		t.Error("a non-key message did something")
	}
	_, msgs = send(m, kt(tea.KeyCtrlC))
	if len(msgs) != 1 {
		t.Fatalf("Ctrl+C: %v", msgs)
	}
	if c, ok := msgs[0].(CancelMsg); !ok || c.Dirty {
		t.Errorf("Ctrl+C sent %#v", msgs[0])
	}
}

func TestStepButtonsMoveAndDelete(t *testing.T) {
	m := New("my-review", review(), Config{Title: "Edit", Slots: testSlots, ParallelMax: 1, Editing: true})
	m.SetSize(120, 40)
	if len(m.steps) < 2 {
		t.Fatalf("review has %d steps", len(m.steps))
	}
	first := m.steps[0].id
	m.sel = 0
	m, _ = m.press(iDown)
	if m.steps[1].id != first || m.sel != 1 {
		t.Errorf("Move down: steps %s,%s sel %d", m.steps[0].id, m.steps[1].id, m.sel)
	}
	m, _ = m.press(iUp)
	if m.steps[0].id != first || m.sel != 0 {
		t.Errorf("Move up: steps %s,%s sel %d", m.steps[0].id, m.steps[1].id, m.sel)
	}
	m, _ = m.press(iUp) // already first: nothing moves
	if m.steps[0].id != first {
		t.Error("Move up on the first step moved it")
	}
	_, cmd := m.press(iDelete)
	if cmd == nil {
		t.Fatal("Delete profile did nothing")
	}
	if _, ok := cmd().(DeleteMsg); !ok {
		t.Errorf("Delete sent %T", cmd())
	}
	found := false
	for _, it := range m.items() {
		if it.kind == iDelete {
			found = true
		}
	}
	if !found {
		t.Error("an edited profile has no Delete button")
	}
}

func TestRemoveStepNamesTheStep(t *testing.T) {
	m := New("p", ai.Profile{Steps: []ai.ProfileStep{{ID: "only", Slot: "main"}}}, cfg(1))
	cmd := m.removeCmd()
	if cmd == nil {
		t.Fatal("no remove command")
	}
	if r := cmd().(RemoveStepMsg); r.Title != "only" || r.Index != 0 {
		t.Errorf("a step without a title is named %q (index %d), want its id", r.Title, r.Index)
	}
	m.RemoveStep(5) // out of range: nothing
	m.RemoveStep(0)
	if len(m.steps) != 0 || m.removeCmd() != nil {
		t.Errorf("after removing the last step: %d steps, remove %v", len(m.steps), m.removeCmd() != nil)
	}
}

func TestFocusHelpersIgnoreStepsThatDoNotExist(t *testing.T) {
	m := New("my-review", review(), cfg(1))
	before := m.foc
	m.FocusModel(-1)
	m.FocusModel(99)
	m.FocusStep(99)
	m.FocusFix(99)
	if m.foc != before {
		t.Errorf("out-of-range focus moved to %v", m.foc)
	}
	m.FocusModel(1)
	if m.foc != (item{iSModel, 1}) || m.sel != 1 {
		t.Errorf("FocusModel(1): %v sel %d", m.foc, m.sel)
	}
	m.FocusFix(0) // step 0's AI is ready: Fix falls back to Model
	if m.foc != (item{iSModel, 0}) {
		t.Errorf("FocusFix on a ready step: %v", m.foc)
	}
	if cmd := m.fixCmd(0); cmd != nil {
		t.Error("Fix on a ready step sent a message")
	}
}

func TestUnknownDepsShowAsWritten(t *testing.T) {
	p := ai.Profile{Steps: []ai.ProfileStep{
		{ID: "a", Slot: "main"},
		{ID: "b", Slot: "main", Deps: []string{"a", "ghost"}},
	}}
	m := New("p", p, cfg(2))
	if got := m.steps[1].deps; got != "1, ghost" {
		t.Errorf("deps field = %q, want %q", got, "1, ghost")
	}
	if msg := m.Errors()["1:deps"]; !strings.Contains(msg, "ghost") {
		t.Errorf("deps error = %q, want it to name ghost", msg)
	}
}

func TestStepErrorsLandOnTheirField(t *testing.T) {
	// An empty prompt for a planner step is the step's "prompt" problem;
	// a step field the form does not show is the step's own problem.
	p := ai.Profile{Steps: []ai.ProfileStep{{ID: "s", Slot: "nope"}}}
	m := New("p", p, cfg(1))
	errs := m.Errors()
	if _, ok := errs["0:model"]; !ok {
		t.Errorf("unknown slot: errors %v, want 0:model", errs)
	}
	if it, msg := m.firstProblem(); msg == "" || it != (item{iSModel, 0}) {
		t.Errorf("first problem %v %q, want the step's model", it, msg)
	}
	ok := New("p", ai.Profile{Steps: []ai.ProfileStep{{ID: "s", Slot: "main"}}}, cfg(1))
	if it, msg := ok.firstProblem(); msg != "" || it != (item{}) {
		t.Errorf("valid form: first problem %v %q", it, msg)
	}
}

func TestQuickAndDeepSlotsRoundTrip(t *testing.T) {
	p := ai.Profile{Steps: []ai.ProfileStep{{ID: "s", Slot: "main"}}, Categories: map[string]string{"quick": "quick", "deep": "local"}}
	m := New("p", p, cfg(1))
	got := m.Profile().Categories
	if got["quick"] != "quick" || got["deep"] != "local" {
		t.Errorf("categories = %v", got)
	}
	m = focusOn(m, item{kind: iCatD})
	m, _ = send(m, kt(tea.KeyLeft), kt(tea.KeyLeft), kt(tea.KeyLeft))
	if _, ok := m.Profile().Categories["deep"]; ok {
		t.Errorf("deep slot cycled to none still saved: %v", m.Profile().Categories)
	}
	if !m.Dirty() {
		t.Error("a changed slot is not dirty")
	}
}
