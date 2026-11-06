package raceview

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func typeText(f Form, s string) Form {
	for _, r := range s {
		if r == ' ' {
			f, _ = f.Update(key("space"))
			continue
		}
		f, _ = f.Update(key(string(r)))
	}
	return f
}

func run(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

var testOpts = []Option{
	{ID: "claude", Title: "Claude Code", Hint: "claude · signed in"},
	{ID: "codex", Title: "Codex", Hint: "codex"},
	{ID: "gemini", Title: "Gemini CLI", Hint: "gemini"},
	{ID: "aider", Title: "Aider", Hint: "aider"},
	{ID: "opencode", Title: "OpenCode", Hint: "opencode"},
	{ID: "amp", Title: "Amp", Disabled: true, Hint: "its screen cannot be read, so the task cannot be typed safely"},
}

// Spec (AGX-4, plan 2.5): Agent: Race… is one card: the task, 2–4 lanes
// from the installed agents, an optional verify command. Tab moves on;
// Enter in a text field moves on (never starts); Space or Enter on a lane
// ticks it; Start needs a task and 2–4 lanes and says what is missing;
// Esc and Cancel close it and nothing starts.
func TestFormFlow(t *testing.T) {
	f := NewForm(testOpts, "", "")
	f.SetSize(150, 42)
	if f.Focus() != FocusTask {
		t.Fatalf("first focus %v", f.Focus())
	}
	f = typeText(f, "fix the toast race")
	f, cmd := f.Update(key("enter"))
	if run(cmd) != nil || f.Focus() != FocusLane(0) {
		t.Fatalf("enter in the task: focus %v", f.Focus())
	}
	f, _ = f.Update(key("space")) // claude
	f, _ = f.Update(key("down"))
	f, _ = f.Update(key("enter")) // codex
	if got := strings.Join(f.Chosen(), ","); got != "claude,codex" {
		t.Errorf("chosen %s", got)
	}
	// A disabled lane cannot be ticked; the card says why.
	for i := 0; i < 4; i++ {
		f, _ = f.Update(key("down"))
	}
	if f.Focus() != FocusLane(5) {
		t.Fatalf("focus %v", f.Focus())
	}
	f, _ = f.Update(key("space"))
	if len(f.Chosen()) != 2 || !strings.Contains(ansi.Strip(f.Box()), "cannot be typed safely") {
		t.Errorf("disabled lane: %v\n%s", f.Chosen(), ansi.Strip(f.Box()))
	}
	// At most 4.
	f.SetFocus(FocusLane(2))
	f, _ = f.Update(key("space"))
	f.SetFocus(FocusLane(3))
	f, _ = f.Update(key("space"))
	f.SetFocus(FocusLane(4))
	f, _ = f.Update(key("space"))
	if len(f.Chosen()) != 4 || !strings.Contains(ansi.Strip(f.Box()), "at most 4 lanes") {
		t.Errorf("fifth lane: %v", f.Chosen())
	}
	f.SetFocus(FocusVerify)
	f = typeText(f, "go test ./...")
	f, cmd = f.Update(key("enter"))
	if run(cmd) != nil || f.Focus() != FocusCancel {
		t.Fatalf("enter in verify: focus %v", f.Focus())
	}
	// Enter on Cancel cancels.
	if _, cmd := f.Update(key("enter")); run(cmd) != (CancelMsg{}) {
		t.Error("Cancel did not cancel")
	}
	f, _ = f.Update(key("tab"))
	if f.Focus() != FocusStart {
		t.Fatalf("focus %v", f.Focus())
	}
	_, cmd = f.Update(key("enter"))
	st, ok := run(cmd).(StartMsg)
	if !ok || st.Task != "fix the toast race" || st.Verify != "go test ./..." || strings.Join(st.Lanes, ",") != "claude,codex,gemini,aider" {
		t.Errorf("start %+v %v", st, ok)
	}
	if _, cmd := f.Update(key("esc")); run(cmd) != (CancelMsg{}) {
		t.Error("Esc did not cancel")
	}
}

// Spec (AGX-4): Start with a missing part does not start; the card says
// what is missing and moves the focus there.
func TestFormStartNeedsTaskAndLanes(t *testing.T) {
	f := NewForm(testOpts, "", "")
	f.SetSize(80, 24)
	f.SetFocus(FocusStart)
	f, cmd := f.Update(key("enter"))
	if run(cmd) != nil || f.Focus() != FocusTask || !strings.Contains(ansi.Strip(f.Box()), "Type the task") {
		t.Errorf("no task: focus %v\n%s", f.Focus(), ansi.Strip(f.Box()))
	}
	f = typeText(f, "x")
	f.SetFocus(FocusLane(0))
	f, _ = f.Update(key("space"))
	f.SetFocus(FocusStart)
	f, cmd = f.Update(key("enter"))
	if run(cmd) != nil || f.Focus() != FocusLane(0) || !strings.Contains(ansi.Strip(f.Box()), "Pick 2 to 4 lanes") {
		t.Errorf("one lane: focus %v\n%s", f.Focus(), ansi.Strip(f.Box()))
	}
	// Backspace edits the field.
	f.SetFocus(FocusTask)
	f, _ = f.Update(key("backspace"))
	if f.Task() != "" {
		t.Errorf("task %q", f.Task())
	}
}

// Spec (AGX-4): the card fits 80x24 with every row inside the screen, and
// the Start button says how many lanes start.
func TestFormFits(t *testing.T) {
	for _, sz := range [][2]int{{150, 42}, {80, 24}, {60, 20}} {
		f := NewForm(testOpts, "make the toast queue thread-safe", "")
		f.SetSize(sz[0], sz[1])
		f.SetFocus(FocusLane(0))
		f, _ = f.Update(key("space"))
		f.SetFocus(FocusLane(1))
		f, _ = f.Update(key("space"))
		box := f.Box()
		lines := strings.Split(box, "\n")
		x, y := f.Origin()
		if len(lines)+y > sz[1] {
			t.Errorf("%v: %d rows at y %d", sz, len(lines), y)
		}
		for _, l := range lines {
			if w := ansi.StringWidth(l); w+x > sz[0] {
				t.Errorf("%v: a row of %d cells at x %d", sz, w, x)
			}
		}
		if !strings.Contains(ansi.Strip(box), "Start 2 lanes") {
			t.Errorf("%v: no Start 2 lanes in\n%s", sz, ansi.Strip(box))
		}
	}
}
