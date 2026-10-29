package profileform

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

func TestPickerSearchKeys(t *testing.T) {
	m := pickModel(oneStep())
	m.OpenPicker(0)
	m, _ = send(m, kt(tea.KeyBackspace)) // empty search: nothing to delete
	if m.pick.q != "" {
		t.Fatalf("Backspace on an empty search: %q", m.pick.q)
	}
	m, _ = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("lla\x07\nma")}, kt(tea.KeySpace))
	if m.pick.q != "llama " {
		t.Errorf("search = %q, want control bytes dropped and a space", m.pick.q)
	}
	m, _ = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\x07")})
	if m.pick.q != "llama " {
		t.Errorf("a lone control byte changed the search to %q", m.pick.q)
	}
	if !strings.Contains(plain(m), "llama-4") {
		t.Errorf("search for llama does not show llama-4:\n%s", plain(m))
	}
	m, _ = send(m, kt(tea.KeyBackspace))
	if m.pick.q != "llama" {
		t.Errorf("Backspace: %q", m.pick.q)
	}
	m, _ = send(m, kt(tea.KeyCtrlU))
	if m.pick.q != "" {
		t.Errorf("Ctrl+U: %q", m.pick.q)
	}
	m, msgs := send(m, kt(tea.KeyCtrlC))
	if _, open := m.PickerOpen(); open || len(msgs) != 0 {
		t.Errorf("Ctrl+C in the picker: open %v, msgs %v (it must only close the picker)", open, msgs)
	}
}

func TestPickerPageKeysStayOnChoosableRows(t *testing.T) {
	m := pickModel(oneStep())
	m.OpenPicker(0)
	rows := m.pickRows()
	m, _ = send(m, kt(tea.KeyPgDown))
	if !rows[m.pick.cur].choosable() {
		t.Errorf("PgDn landed on a heading (row %d)", m.pick.cur)
	}
	after := m.pick.cur
	m, _ = send(m, kt(tea.KeyPgUp))
	if !rows[m.pick.cur].choosable() || m.pick.cur == after {
		t.Errorf("PgUp: row %d", m.pick.cur)
	}
	m, _ = send(m, kt(tea.KeyCtrlN), kt(tea.KeyCtrlP))
	if !rows[m.pick.cur].choosable() {
		t.Errorf("Ctrl+N/Ctrl+P landed on a heading")
	}
}

func TestPickerChooseOutOfRangeDoesNothing(t *testing.T) {
	m := pickModel(oneStep())
	m.OpenPicker(0)
	for _, k := range []int{-1, 999} {
		if m2, cmd := m.pickChoose(k); cmd != nil || m2.steps[0].ch != m.steps[0].ch {
			t.Errorf("pickChoose(%d) changed the step", k)
		}
	}
}

func TestPickerMouseWheelAndOtherButtons(t *testing.T) {
	m := pickModel(oneStep())
	m.SetSize(80, 24) // a short window: the list scrolls
	m.OpenPicker(0)
	for i := 0; i < 50; i++ {
		m, _ = m.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonWheelDown)
	}
	bottom := m.pick.scroll
	if bottom == 0 {
		t.Fatal("wheel down did not scroll a long list")
	}
	m, _ = m.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonWheelDown)
	if m.pick.scroll != bottom {
		t.Errorf("wheel past the end: scroll %d, want %d", m.pick.scroll, bottom)
	}
	for i := 0; i < 50; i++ {
		m, _ = m.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonWheelUp)
	}
	if m.pick.scroll != 0 {
		t.Errorf("wheel up: scroll %d", m.pick.scroll)
	}
	before := m.steps[0].ch
	r := m.ButtonRects()[1]
	if m2, cmd := m.HandleMouse(r.X+1, r.Y, tea.MouseActionPress, tea.MouseButtonRight); cmd != nil || m2.steps[0].ch != before {
		t.Error("right click chose a model")
	}
	// A click in the card's left margin chooses nothing.
	ox, _ := m.Origin()
	if m2, _ := m.HandleMouse(ox+1, r.Y, tea.MouseActionPress, tea.MouseButtonLeft); m2.steps[0].ch != before {
		t.Error("a click in the margin chose a model")
	}
	// A click on a row that is not a model (outside every row) does nothing.
	if m2, _ := m.HandleMouse(ox+10, 0, tea.MouseActionPress, tea.MouseButtonLeft); m2.steps[0].ch != before {
		t.Error("a click above the list chose a model")
	}
}

func TestModelArrowsStartFromAnUnlistedChoice(t *testing.T) {
	// A hand-edited profile with a category the picker does not list.
	p := ai.Profile{Steps: []ai.ProfileStep{{ID: "a", Category: "weird"}}}
	start := func() Model { return focusOn(pickModel(p), item{iSModel, 0}) }
	right, _ := send(start(), kt(tea.KeyRight))
	if right.steps[0].ch != (choice{}) { // the first choice: same model as the chat
		t.Errorf("Right from an unlisted choice: %+v, want the first choice", right.steps[0].ch)
	}
	left, _ := send(start(), kt(tea.KeyLeft))
	if left.steps[0].ch != (choice{slot: "gpt"}) { // the last: the named model from config.json
		t.Errorf("Left from an unlisted choice: %+v, want the last choice (slot gpt)", left.steps[0].ch)
	}
}

func TestFormMouseWheelAndOtherButtons(t *testing.T) {
	m := New("my-review", review(), cfg(2))
	m.SetSize(80, 20) // short: the body scrolls
	m.sel = 1
	for i := 0; i < 40; i++ {
		m, _ = m.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonWheelDown)
	}
	bottom := m.scroll
	if bottom == 0 {
		t.Fatal("wheel down did not scroll the form")
	}
	m, _ = m.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonWheelDown)
	if m.scroll != bottom {
		t.Errorf("wheel past the end: %d, want %d", m.scroll, bottom)
	}
	m, _ = m.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonWheelUp)
	if m.scroll != bottom-2 {
		t.Errorf("wheel up: %d, want %d", m.scroll, bottom-2)
	}
	before := m.foc
	if m2, cmd := m.HandleMouse(10, 10, tea.MouseActionRelease, tea.MouseButtonLeft); cmd != nil || m2.foc != before {
		t.Error("a button release did something")
	}
	if m2, cmd := m.HandleMouse(10, 10, tea.MouseActionPress, tea.MouseButtonRight); cmd != nil || m2.foc != before {
		t.Error("a right click did something")
	}
}

func TestFormClicksOnStepHeadPlanAndChoices(t *testing.T) {
	m := New("my-review", review(), cfg(1))
	m.SetSize(120, 50)
	click := func(match func(target) bool) bool {
		_, tg := m.frame()
		ox, oy := m.Origin()
		for _, t0 := range tg {
			if match(t0) {
				m, _ = m.HandleMouse(ox+1+t0.x, oy+1+t0.y, tea.MouseActionPress, tea.MouseButtonLeft)
				return true
			}
		}
		return false
	}
	if !click(func(t0 target) bool { return t0.it == item{iStepHead, 1} }) {
		t.Fatal("no step 2 head")
	}
	if m.sel != 1 {
		t.Errorf("click on step 2's head: sel %d", m.sel)
	}
	plan := m.steps[1].plan
	if !click(func(t0 target) bool { return t0.it == item{iSPlan, 1} }) {
		t.Fatal("no planner box")
	}
	if m.steps[1].plan == plan {
		t.Error("click on the planner box did not toggle it")
	}
	q := m.catQ
	if !click(func(t0 target) bool { return t0.it.kind == iCatQ && t0.opt == 0 }) {
		t.Fatal("no quick slot choice")
	}
	if m.catQ == q {
		t.Error("click on the quick slot did not change it")
	}
	q = m.catQ
	if click(func(t0 target) bool { return t0.it.kind == iCatQ && t0.opt == -1 }) && m.catQ == q {
		t.Error("click on the quick slot's ‹ did not change it")
	}
	if !click(func(t0 target) bool { return t0.it.kind == iSave }) {
		t.Fatal("no Save button")
	}
	if m.foc.kind == iSave && !m.Valid() {
		t.Error("a click on an inactive Save stays on Save; it should show the problem")
	}
}
