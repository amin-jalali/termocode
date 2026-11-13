package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Coverage of debug_console.go: output buffering, input editing, the REPL
// keys and rendering.

func TestCovDebugConsoleBuffer(t *testing.T) {
	var nilc *debugConsole
	nilc.AppendOutput("stdout", "x")
	nilc.Clear()
	nilc.Scroll(1)
	if nilc.Lines() != nil || nilc.Seq() != 0 {
		t.Error("nil console")
	}
	if out, cut := nilc.Tail(3); out != nil || cut {
		t.Error("nil tail")
	}
	c := &debugConsole{} // partial map created on demand
	c.AppendOutput("", "")
	c.AppendOutput("", "one\r\ntwo")
	c.AppendOutput("stderr", "err-tail")
	c.AppendOutput("", " more\nthree\n")
	got := c.Lines()
	texts := []string{}
	for _, l := range got {
		texts = append(texts, l.Category+":"+l.Text)
	}
	want := []string{"console:one", "console:two more", "console:three", "stderr:err-tail"}
	if strings.Join(texts, "|") != strings.Join(want, "|") {
		t.Errorf("lines %v", texts)
	}
	seq := c.Seq()
	// the tail keeps the newest lines, partials last
	out, cut := c.Tail(2)
	if len(out) != 2 || out[1].Category != "stderr" || !cut {
		t.Errorf("tail %v cut=%v", out, cut)
	}
	if out, cut := c.Tail(10); len(out) != 4 || cut {
		t.Errorf("tail %v cut=%v", out, cut)
	}
	// scrolled output keeps its place while new lines arrive
	c.viewH = 2
	c.Scroll(-1)
	before := c.scroll
	c.AppendOutput("stdout", "a\nb\n")
	if c.scroll <= before-1 {
		t.Errorf("scroll %d -> %d", before, c.scroll)
	}
	// the cap drops the oldest
	for i := 0; i < debugConsoleMaxLines+10; i++ {
		c.AppendOutput("stdout", "x\n")
	}
	if len(c.lines) != debugConsoleMaxLines {
		t.Errorf("lines %d", len(c.lines))
	}
	c.Clear()
	if len(c.Lines()) != 0 || c.Seq() <= seq {
		t.Error("clear")
	}
	if c.window(0) != nil || c.window(3) != nil && len(c.window(3)) != 0 {
		t.Error("window of an empty console")
	}
	(*debugConsole)(nil).window(3)
}

func TestCovDebugConsoleInputAndKeys(t *testing.T) {
	m := Model{w: 100, h: 30, debug: newDebugState()}
	c := m.debug.console
	press := func(k string) (cmdNil bool, handled bool) {
		cmd, ok := debugConsoleKey(&m, keyOf(k))
		return cmd == nil, ok
	}
	for _, k := range []string{"a", "b", " ", "c"} {
		press(k)
	}
	if string(c.input) != "ab c" || c.cursor != 4 {
		t.Fatalf("input %q cur %d", string(c.input), c.cursor)
	}
	for _, k := range []string{"left", "ctrl+b", "right", "ctrl+f", "home", "ctrl+a", "end", "ctrl+e"} {
		press(k)
	}
	press("home")
	press("backspace") // at the start: nothing
	press("delete")    // deletes 'a'
	press("end")
	press("backspace")
	press("ctrl+d") // at the end: nothing
	press("ctrl+h")
	if string(c.input) != "b" {
		t.Errorf("input %q", string(c.input))
	}
	press("right") // at the end: stays
	// Enter evaluates: no session -> an error line; history keeps it once
	press("enter")
	c.insert([]rune("x+1"))
	press("enter")
	c.insert([]rune("x+1"))
	press("enter")
	if len(c.history) != 2 {
		t.Errorf("history %v", c.history)
	}
	press("up")
	press("up")
	press("up") // clamps at the oldest
	if string(c.input) != "b" {
		t.Errorf("oldest = %q", string(c.input))
	}
	press("down")
	press("down")
	press("down") // past the newest: a fresh line
	if len(c.input) != 0 {
		t.Errorf("fresh line %q", string(c.input))
	}
	(&debugConsole{}).historyMove(-1) // no history
	c.insert([]rune("zzz"))
	press("ctrl+u")
	if len(c.input) != 0 {
		t.Error("ctrl+u")
	}
	for _, k := range []string{"pgup", "pgdown", "ctrl+l"} {
		press(k)
	}
	if _, ok := debugConsoleKey(&m, tea.KeyMsg{Type: tea.KeyF9}); ok {
		t.Error("function keys are not handled")
	}
	// the REPL with a session: queues an evaluate (no editor: nothing to run)
	m.dapSessionActive = true
	if m.debugEvalRepl("1+1") != nil || m.debugEvalRepl("  ") != nil {
		t.Error("repl returns no command")
	}
	for _, cat := range []string{"stderr", "console", "telemetry", "important", "input", "result", "stdout"} {
		_ = debugConsoleStyle(cat)
	}
}

func TestCovDebugConsoleRender(t *testing.T) {
	m := Model{w: 100, h: 30, debug: newDebugState()}
	c := m.debug.console
	for _, active := range []bool{false, true} {
		m.dapSessionActive = active
		rows := renderDebugConsole(&m, 60, 6)
		if len(rows) != 6 || !strings.Contains(rows[0], "No ") {
			t.Errorf("empty console rows %q", rows)
		}
	}
	c.AppendOutput("stdout", "hello\n"+strings.Repeat("w", 200)+"\n")
	c.AppendOutput("stderr", "boom\n")
	for _, h := range []int{1, 2, 3, 8} {
		for _, focused := range []bool{false, true} {
			m.panelFocused, m.panelActive = focused, panelKindDebugConsole
			if got := renderDebugConsole(&m, 40, h); len(got) != h && !(h == 1 && len(got) == 1) {
				t.Errorf("h=%d rows=%d", h, len(got))
			}
		}
	}
	// input longer than the width scrolls and keeps the cursor visible
	m.panelFocused, m.panelActive = true, panelKindDebugConsole
	c.insert([]rune(strings.Repeat("abc ", 30)))
	_ = renderDebugConsole(&m, 30, 4)
	c.cursor = 2
	_ = renderDebugConsole(&m, 30, 4)
	_ = renderDebugConsole(&m, 4, 4)
	m.debug = nil
	_ = renderDebugConsole(&m, 40, 4)
	// show it on a real editor
	rm, _ := goRepoModel(t)
	rm.showDebugConsole(true)
	rm.showDebugConsole(false)
	panelKinds[panelKindDebugConsole].Scroll(&rm, 2)
}
