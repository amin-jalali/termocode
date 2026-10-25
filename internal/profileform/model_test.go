package profileform

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

var testSlots = []Slot{{"main", true}, {"quick", true}, {"local", true}, {"gpt", false}}

func cfg(par int) Config {
	return Config{Title: "New profile", Slots: testSlots, ParallelMax: par}
}

func review() ai.Profile {
	for _, p := range ai.BuiltinProfiles() {
		if p.Name == ai.ProfileReview {
			return p
		}
	}
	panic("no review")
}

// keys sends keys: a one-rune string is typed, names like "tab", "enter",
// "esc", "down", "backspace" are keys.
func keys(m Model, ks ...string) (Model, []tea.Msg) {
	var got []tea.Msg
	for _, k := range ks {
		var km tea.KeyMsg
		switch k {
		case "tab":
			km = tea.KeyMsg{Type: tea.KeyTab}
		case "shift+tab":
			km = tea.KeyMsg{Type: tea.KeyShiftTab}
		case "enter":
			km = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			km = tea.KeyMsg{Type: tea.KeyEsc}
		case "down":
			km = tea.KeyMsg{Type: tea.KeyDown}
		case "up":
			km = tea.KeyMsg{Type: tea.KeyUp}
		case "left":
			km = tea.KeyMsg{Type: tea.KeyLeft}
		case "right":
			km = tea.KeyMsg{Type: tea.KeyRight}
		case "backspace":
			km = tea.KeyMsg{Type: tea.KeyBackspace}
		default:
			km = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		var cmd tea.Cmd
		m, cmd = m.Update(km)
		if cmd != nil {
			got = append(got, cmd())
		}
	}
	return m, got
}

func focusOn(m Model, it item) Model {
	m.setFocus(it)
	return m
}

func plain(m Model) string { return ansi.Strip(m.Box()) }

// Spec: opening the form on a profile and changing nothing gives the same
// profile back (so Save after no edit writes what a copy would), and the
// form is clean.
func TestFormRoundTrip(t *testing.T) {
	m := New("my-review", review(), cfg(1))
	m.SetSize(120, 40)
	want := review()
	want.Name, want.Source = "my-review", ""
	if got := m.Profile(); !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip:\n got %#v\nwant %#v", got, want)
	}
	if m.Dirty() || !m.Valid() {
		t.Errorf("dirty=%v valid=%v errors=%v", m.Dirty(), m.Valid(), m.Errors())
	}
}

// Spec: typing in the name field changes the profile; Esc ends editing,
// a second Esc cancels and says the form was changed.
func TestFormTypeAndCancel(t *testing.T) {
	m := New("x", review(), cfg(1))
	m.SetSize(120, 40)
	m, _ = keys(m, "enter", "backspace", "y", "z")
	if m.Name() != "yz" {
		t.Fatalf("name %q", m.Name())
	}
	m, got := keys(m, "esc")
	if len(got) != 0 || m.editing {
		t.Fatalf("first Esc only ends editing: %v", got)
	}
	_, got = keys(m, "esc")
	if len(got) != 1 || got[0] != (CancelMsg{Dirty: true}) {
		t.Fatalf("second Esc: %#v", got)
	}
}

// Spec: the rules of ParseProfiles show inline and Save does nothing
// while one fails: duplicate step id, more than 12 steps, two planners,
// "waits for" without ai_parallel_max.
func TestFormValidationInline(t *testing.T) {
	has := func(m Model, key string) bool { _, ok := m.Errors()[key]; return ok }
	save := func(m Model) []tea.Msg {
		m = focusOn(m, item{kind: iSave})
		_, got := keys(m, "enter")
		return got
	}

	m := New("p", review(), cfg(1))
	m.SetSize(120, 40)
	m.steps[1].id = m.steps[0].id
	if !has(m, "1:id") || m.Valid() {
		t.Errorf("duplicate id: %v", m.Errors())
	}
	if got := save(m); len(got) != 0 {
		t.Errorf("Save while invalid emitted %v", got)
	}
	if !strings.Contains(plain(m), `duplicate step id "reviewer"`) {
		t.Errorf("error not shown:\n%s", plain(m))
	}

	m = New("p", review(), cfg(1))
	m.SetSize(120, 40)
	for len(m.steps) <= ai.MaxProfileSteps {
		m.AddStep()
	}
	if !has(m, "steps") || !strings.Contains(m.Errors()["steps"], "too many steps") {
		t.Errorf("13 steps: %v", m.Errors())
	}

	m = New("p", review(), cfg(1))
	m.steps[0].plan, m.steps[1].plan = true, true
	if !has(m, "plan") {
		t.Errorf("two planners: %v", m.Errors())
	}

	m = New("p", review(), cfg(1))
	m.steps[1].deps = "1"
	if !has(m, "1:deps") || !strings.Contains(m.Errors()["1:deps"], "ai_parallel_max") {
		t.Errorf("deps without parallel_max: %v", m.Errors())
	}
	m = New("p", review(), cfg(3))
	m.steps[1].deps = "1"
	if !m.Valid() {
		t.Errorf("deps with parallel 3: %v", m.Errors())
	}
	m.steps[1].deps = "7"
	if !has(m, "1:deps") {
		t.Errorf("deps on a step that does not exist: %v", m.Errors())
	}

	m = New("default", review(), cfg(1))
	if !has(m, "name") {
		t.Errorf("built-in name: %v", m.Errors())
	}
	m = New("p", review(), Config{Slots: testSlots, ParallelMax: 1, Taken: map[string]bool{"p": true}})
	if !has(m, "name") {
		t.Errorf("taken name: %v", m.Errors())
	}
	m = New("p", review(), Config{Slots: testSlots, ParallelMax: 1, Taken: map[string]bool{"p": true}, Editing: true})
	if has(m, "name") || !m.Replace() {
		t.Errorf("editing keeps the name: %v", m.Errors())
	}
	m = New("p", review(), cfg(1))
	m.budget = "abc"
	if !has(m, "budget") {
		t.Errorf("budget: %v", m.Errors())
	}
	m.budget = "-1"
	if !has(m, "budget") {
		t.Errorf("negative budget: %v", m.Errors())
	}
}

// Spec: a valid form sends SaveMsg.
func TestFormSave(t *testing.T) {
	m := New("p", review(), cfg(1))
	m.SetSize(120, 40)
	m = focusOn(m, item{kind: iSave})
	_, got := keys(m, "enter")
	if len(got) != 1 || got[0] != (SaveMsg{}) {
		t.Fatalf("got %#v", got)
	}
}

// Spec: add, move and remove steps. Remove asks first (RemoveStepMsg);
// the step is gone only after RemoveStep. "Waits for" keeps pointing at
// the same steps when they move.
func TestFormStepOps(t *testing.T) {
	m := New("p", review(), cfg(3))
	m.SetSize(120, 40)
	m, _ = keys(m, "a")
	if len(m.steps) != 3 || m.sel != 2 || m.foc != (item{iSTitle, 2}) {
		t.Fatalf("add: %d steps sel %d foc %v", len(m.steps), m.sel, m.foc)
	}
	if m.steps[2].id != "step-1" {
		t.Errorf("new id %q", m.steps[2].id)
	}
	m.steps[2].deps = "1"
	m = focusOn(m, item{iStepHead, 2})
	m, _ = keys(m, "K")
	if m.steps[1].id != "step-1" || m.sel != 1 {
		t.Fatalf("move up: %v", m.steps)
	}
	if got := m.Profile().Steps[1].Deps; !reflect.DeepEqual(got, []string{"reviewer"}) {
		t.Errorf("deps after move: %v", got)
	}
	m, _ = keys(m, "J")
	if m.steps[2].id != "step-1" || m.steps[2].deps != "1" {
		t.Errorf("move down: %+v", m.steps[2])
	}
	m, _ = keys(m, "J") // at the end: nothing
	if m.sel != 2 {
		t.Errorf("sel %d", m.sel)
	}
	m, got := keys(m, "x")
	if len(got) != 1 || len(m.steps) != 3 {
		t.Fatalf("remove asks first: %v steps %d", got, len(m.steps))
	}
	if rm, ok := got[0].(RemoveStepMsg); !ok || rm.Index != 2 || rm.Title != "Step 3" {
		t.Fatalf("msg %#v", got[0])
	}
	m.RemoveStep(0) // the reviewer: the others lose it as a dep
	if len(m.steps) != 2 || m.steps[1].deps != "" {
		t.Errorf("after remove: %+v", m.steps)
	}
	if m.sel < 0 || m.sel >= len(m.steps) {
		t.Errorf("sel %d", m.sel)
	}
}

// Spec: the model choice offers every slot and both categories; a slot
// shows its key state (a mark, never a key); the tools choice cycles.
func TestFormChoices(t *testing.T) {
	m := New("p", review(), cfg(1))
	m.SetSize(120, 40)
	m = focusOn(m, item{iSModel, 0})
	seen := map[string]bool{}
	for i := 0; i < len(m.opts); i++ {
		m, _ = keys(m, "right")
		seen[m.opts[m.steps[0].model].label] = true
	}
	for _, want := range []string{"main", "quick", "local", "gpt", "category quick", "category deep", "default (main)"} {
		if !seen[want] {
			t.Errorf("no option %q (%v)", want, seen)
		}
	}
	m.steps[0].model = m.modelIndex("gpt", "")
	if out := plain(m); !strings.Contains(out, "gpt") && strings.Contains(out, "no key") {
		t.Errorf("key state missing:\n%s", out)
	}
	m = focusOn(m, item{iSTools, 0})
	m, _ = keys(m, "right")
	if m.Profile().Steps[0].Tools.Preset != agentdag.PresetAll {
		t.Errorf("tools %v", m.Profile().Steps[0].Tools)
	}
	// Categories reach the profile.
	m = focusOn(m, item{kind: iCatD})
	m, _ = keys(m, "right", "right", "right") // default → main, quick, local
	if got := m.Profile().Categories[agentdag.CategoryDeep]; got != "local" {
		t.Errorf("deep → %q", got)
	}
}

// Spec: the form keeps what it does not show (tools allow / deny,
// max_steps, output) of an edited step.
func TestFormKeepsHiddenStepFields(t *testing.T) {
	p := ai.Profile{Steps: []ai.ProfileStep{{ID: "a", MaxSteps: 7, Output: "folded",
		Tools: agentdag.ToolRule{Preset: "read-only", Allow: []string{"x"}}}}}
	m := New("p", p, cfg(1))
	m = focusOn(m, item{iSTools, 0})
	m, _ = keys(m, "left")
	st := m.Profile().Steps[0]
	if st.MaxSteps != 7 || st.Output != "folded" || !reflect.DeepEqual(st.Tools.Allow, []string{"x"}) || st.Tools.Preset != "none" {
		t.Errorf("%+v", st)
	}
}

// Spec: the prompt is multi-line: Enter adds a line while editing it.
func TestFormPromptMultiline(t *testing.T) {
	m := New("p", review(), cfg(1))
	m.SetSize(120, 40)
	m = focusOn(m, item{iSPrompt, 0})
	m, _ = keys(m, "enter") // start editing, caret at the end
	m, _ = keys(m, "enter", "N", "e", "w")
	if !strings.HasSuffix(m.steps[0].system, "\nNew") {
		t.Errorf("prompt %q", m.steps[0].system)
	}
	if !m.editing {
		t.Error("Enter in a prompt must not end editing")
	}
}

// Spec: 80x24 (and smaller) never overflows: every row is at most the
// window wide, the card is at most the window high, with 12 steps and
// every field filled, and the focused field is visible after moving
// through all items.
func TestFormFitsNarrow(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {60, 20}, {200, 50}} {
		w, h := size[0], size[1]
		p := ai.Profile{Title: strings.Repeat("long title ", 12), Steps: nil}
		for i := 0; i < 12; i++ {
			p.Steps = append(p.Steps, ai.ProfileStep{ID: "s" + string(rune('a'+i)), Title: strings.Repeat("step ", 20),
				System: strings.Repeat("You review the code carefully. ", 10)})
		}
		m := New(strings.Repeat("n", 40), p, Config{Title: "Edit profile", Slots: testSlots, ParallelMax: 3, Editing: true})
		m.SetSize(w, h)
		for i := 0; i < 80; i++ {
			m, _ = keys(m, "tab")
			out := plain(m)
			lines := strings.Split(out, "\n")
			if len(lines) > h {
				t.Fatalf("%dx%d: %d rows", w, h, len(lines))
			}
			for _, l := range lines {
				if runewidth.StringWidth(l) > w {
					t.Fatalf("%dx%d: row too wide (%d): %q", w, h, runewidth.StringWidth(l), l)
				}
			}
			if !m.focusShown() {
				t.Fatalf("%dx%d: focus %v not visible (scroll %d)", w, h, m.foc, m.scroll)
			}
		}
	}
}

// focusShown reports that the focused item has a row on screen.
func (m Model) focusShown() bool {
	b := m.build()
	view := m.viewRows(b)
	for _, r := range b.ranges {
		if r.it == m.foc {
			return r.y0 >= m.scroll && r.y0 < m.scroll+view
		}
	}
	return m.foc.kind.button() // buttons are always in the footer
}

// Spec: a click on a button presses it; a click on a text field starts
// editing; a click on a tools radio selects it; the wheel scrolls.
func TestFormMouse(t *testing.T) {
	m := New("p", review(), cfg(1))
	m.SetSize(100, 40)
	var add ButtonRect
	for _, b := range m.ButtonRects() {
		if b.Title == "Add step" {
			add = b
		}
	}
	if add.W == 0 {
		t.Fatal("no Add step button")
	}
	m, _ = m.HandleMouse(add.X+2, add.Y, tea.MouseActionPress, tea.MouseButtonLeft)
	if len(m.steps) != 3 {
		t.Fatalf("click on Add step: %d steps", len(m.steps))
	}
	_, tg := m.frame()
	ox, oy := m.Origin()
	for _, t0 := range tg {
		if t0.it == (item{iSTitle, 2}) {
			m, _ = m.HandleMouse(ox+1+t0.x+1, oy+1+t0.y, tea.MouseActionPress, tea.MouseButtonLeft)
			break
		}
	}
	if !m.editing || m.foc != (item{iSTitle, 2}) {
		t.Errorf("click on a text field: editing=%v foc=%v", m.editing, m.foc)
	}
	_, tg = m.frame()
	for _, t0 := range tg {
		if t0.it.kind == iSTools && t0.opt == 10 {
			m, _ = m.HandleMouse(ox+1+t0.x, oy+1+t0.y, tea.MouseActionPress, tea.MouseButtonLeft)
			break
		}
	}
	if m.steps[2].tools != 0 {
		t.Errorf("click on the none radio: %d", m.steps[2].tools)
	}
}

// Spec: a form that opens with a problem starts on it (its step open) and
// says in the footer why Save… is inactive, even when the field is
// scrolled out of view.
func TestFormOpensOnProblem(t *testing.T) {
	p := ai.Profile{Steps: []ai.ProfileStep{{ID: "a", Title: "A"}, {ID: "a", Title: "B"}}}
	m := New("p", p, cfg(1))
	m.SetSize(80, 24)
	if m.foc != (item{iSID, 1}) || m.sel != 1 {
		t.Fatalf("focus %v sel %d", m.foc, m.sel)
	}
	out := plain(m)
	if strings.Count(out, `duplicate step id "a"`) < 2 { // under the field and in the footer
		t.Errorf("problem not shown twice:\n%s", out)
	}
}
