package recents

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// rowOf finds the screen row of the line that shows text in m.View().
func rowOf(t *testing.T, m Model, text string) int {
	t.Helper()
	for y, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(ansi.Strip(line), text) {
			return y
		}
	}
	t.Fatalf("%q not on screen:\n%s", text, ansi.Strip(m.View()))
	return -1
}

func sized(entries []Entry, now time.Time, w, h int) Model {
	m := New(entries, now)
	m.SetSize(w, h)
	return m
}

func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("want a command, got nil")
	}
	return cmd()
}

func TestClickOnRowSelectsThatFile(t *testing.T) {
	now := time.Now()
	m := sized(sampleEntries(now), now, 100, 40)
	y := rowOf(t, m, "view.go")
	m, cmd := m.HandleMouse(50, y, tea.MouseActionPress, tea.MouseButtonLeft)
	sel, ok := runCmd(t, cmd).(SelectMsg)
	if !ok || sel.Path != "/home/user/proj/internal/app/view.go" {
		t.Fatalf("got %#v", sel)
	}
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
}

func TestHoverMovesHighlight(t *testing.T) {
	now := time.Now()
	m := sized(sampleEntries(now), now, 100, 40)
	y := rowOf(t, m, "todo.md")
	m, cmd := m.HandleMouse(50, y, tea.MouseActionMotion, tea.MouseButtonNone)
	if cmd != nil || m.cursor != 3 {
		t.Fatalf("cursor = %d cmd = %v, want 3 and no command", m.cursor, cmd)
	}
	// Hover over the header row does not move it.
	m, _ = m.HandleMouse(50, rowOf(t, m, "Open Recent File"), tea.MouseActionMotion, tea.MouseButtonNone)
	if m.cursor != 3 {
		t.Fatalf("hover on header moved cursor to %d", m.cursor)
	}
}

func TestWheelMovesCursorInsideBox(t *testing.T) {
	now := time.Now()
	m := sized(sampleEntries(now), now, 100, 40)
	y := rowOf(t, m, "main.go")
	m, _ = m.HandleMouse(50, y, tea.MouseActionPress, tea.MouseButtonWheelUp)
	if m.cursor != 0 {
		t.Fatalf("wheel up at top moved to %d", m.cursor)
	}
	for i := 0; i < 10; i++ {
		m, _ = m.HandleMouse(50, y, tea.MouseActionPress, tea.MouseButtonWheelDown)
	}
	if m.cursor != 3 {
		t.Fatalf("wheel down stops at last row, got %d", m.cursor)
	}
	m, _ = m.HandleMouse(50, y, tea.MouseActionPress, tea.MouseButtonWheelUp)
	if m.cursor != 2 {
		t.Fatalf("wheel up = %d, want 2", m.cursor)
	}
}

func TestMouseOutsideOrOffListDoesNothing(t *testing.T) {
	now := time.Now()
	m := sized(sampleEntries(now), now, 100, 40)
	for _, c := range []struct {
		name   string
		x, y   int
		action tea.MouseAction
		button tea.MouseButton
	}{
		{"outside box", 0, 0, tea.MouseActionPress, tea.MouseButtonLeft},
		{"header row", 50, rowOf(t, m, "Open Recent File"), tea.MouseActionPress, tea.MouseButtonLeft},
		{"footer row", 50, rowOf(t, m, "Navigate"), tea.MouseActionPress, tea.MouseButtonLeft},
		{"right button on a row", 50, rowOf(t, m, "todo.md"), tea.MouseActionPress, tea.MouseButtonRight},
		{"release on a row", 50, rowOf(t, m, "todo.md"), tea.MouseActionRelease, tea.MouseButtonLeft},
	} {
		got, cmd := m.HandleMouse(c.x, c.y, c.action, c.button)
		if cmd != nil || got.cursor != 0 {
			t.Errorf("%s: cmd = %v cursor = %d", c.name, cmd, got.cursor)
		}
	}
	unsized := New(sampleEntries(now), now)
	if _, cmd := unsized.HandleMouse(1, 1, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil {
		t.Fatal("a modal with no size took a click")
	}
}

func TestClickOnBlankRowBelowFewMatchesDoesNothing(t *testing.T) {
	now := time.Now()
	// 1 match in a short window: the list keeps 1 row, so clicking the
	// row under it (the hairline) does nothing.
	m := sized(sampleEntries(now), now, 100, 40)
	m, _ = m.Update(runeKey("todo"))
	y := rowOf(t, m, "todo.md")
	if _, cmd := m.HandleMouse(50, y+1, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil {
		t.Fatal("click below the only match selected something")
	}
	// No matches: the empty hint row is not a file.
	m, _ = m.Update(runeKey("zzz"))
	y = rowOf(t, m, "No recent files found")
	if _, cmd := m.HandleMouse(50, y, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil {
		t.Fatal("click on the empty hint selected something")
	}
}

func manyEntries(now time.Time, n int) []Entry {
	out := make([]Entry, n)
	for i := range out {
		out[i] = Entry{Path: fmt.Sprintf("/w/dir/file%02d.go", i), OpenedAt: now.Add(-time.Duration(i) * time.Minute)}
	}
	return out
}

func TestScrolledListClickAndHoverUseTheVisibleWindow(t *testing.T) {
	now := time.Now()
	m := sized(manyEntries(now, 30), now, 100, 40)
	for i := 0; i < 20; i++ {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	view := ansi.Strip(m.View())
	if strings.Contains(view, "file00.go") || !strings.Contains(view, "file20.go") {
		t.Fatalf("list did not scroll:\n%s", view)
	}
	y := rowOf(t, m, "file15.go")
	m2, _ := m.HandleMouse(50, y, tea.MouseActionMotion, tea.MouseButtonNone)
	if m2.cursor != 15 {
		t.Fatalf("hover cursor = %d, want 15", m2.cursor)
	}
	_, cmd := m.HandleMouse(50, y, tea.MouseActionPress, tea.MouseButtonLeft)
	if sel := runCmd(t, cmd).(SelectMsg); sel.Path != "/w/dir/file15.go" {
		t.Fatalf("clicked %q, want file15.go", sel.Path)
	}
}

func TestBlinkTogglesCursorAndKeyShowsIt(t *testing.T) {
	now := time.Now()
	m := sized(sampleEntries(now), now, 100, 40)
	if m.Init() == nil {
		t.Fatal("Init must start the blink")
	}
	m, cmd := m.Update(blinkMsg{})
	if m.cursorVisible || cmd == nil {
		t.Fatalf("blink: visible = %v cmd = %v", m.cursorVisible, cmd)
	}
	if strings.Contains(m.View(), "▎") {
		t.Fatal("hidden cursor still drawn")
	}
	m, _ = m.Update(tea.WindowSizeMsg{})
	if !m.cursorVisible || !strings.Contains(m.View(), "▎") {
		t.Fatal("any other message shows the cursor again")
	}
}

func TestCtrlKeysSpaceAndMouseCodes(t *testing.T) {
	now := time.Now()
	m := sized(sampleEntries(now), now, 100, 40)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.cursor != 1 {
		t.Fatalf("ctrl+n/ctrl+p cursor = %d, want 1", m.cursor)
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c must close")
	} else if _, ok := cmd().(CloseMsg); !ok {
		t.Fatal("ctrl+c did not send CloseMsg")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("35;70"), Alt: true})
	if m.input != "" {
		t.Fatalf("alt mouse code typed %q", m.input)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Alt: true})
	if m.input != "x" {
		t.Fatalf("alt+x should type x, got %q", m.input)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if m.input != "x " {
		t.Fatalf("space: input = %q", m.input)
	}
	// A key with no meaning changes nothing.
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if cmd != nil || m2.input != m.input {
		t.Fatal("tab changed the modal")
	}
	// Backspace on an empty field is fine.
	empty := New(nil, now)
	if empty, _ = empty.Update(tea.KeyMsg{Type: tea.KeyBackspace}); empty.input != "" {
		t.Fatal("backspace on empty field")
	}
}

func boxDims(m Model) (w, h int) {
	lines := strings.Split(ansi.Strip(m.Box()), "\n")
	return ansi.StringWidth(lines[0]), len(lines)
}

func TestBoxSizeFollowsMatchesAndScreen(t *testing.T) {
	now := time.Now()
	m := sized(sampleEntries(now), now, 100, 40)
	if w, h := boxDims(m); w != 66 || h != 11+4 {
		t.Fatalf("box %dx%d, want 66x15", w, h)
	}
	// A tiny screen keeps the box at least 40 wide and 3 rows of list.
	small := sized(manyEntries(now, 30), now, 20, 10)
	if w, h := boxDims(small); w != 42 || h != 14 {
		t.Fatalf("small box %dx%d, want 42x14", w, h)
	}
	// A tall screen caps the list at 12 rows.
	tall := sized(manyEntries(now, 30), now, 100, 100)
	if _, h := boxDims(tall); h != 11+12 {
		t.Fatalf("tall box height %d", h)
	}
}

func TestNarrowBoxStillFitsEveryRow(t *testing.T) {
	now := time.Now()
	entries := []Entry{
		{Path: "/a/very/long/folder/name/that/goes/on/and/on/and/on/some_really_long_file_name_here.go", OpenedAt: now.Add(-10 * 24 * time.Hour)},
		{Path: "plain.txt", OpenedAt: now.Add(time.Hour)},
	}
	m := sized(entries, now, 44, 30)
	m, _ = m.Update(runeKey(strings.Repeat("q", 60)))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	for i := 0; i < 60; i++ {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	box := ansi.Strip(m.Box())
	for _, line := range strings.Split(box, "\n") {
		if w := ansi.StringWidth(line); w != 42 {
			t.Fatalf("row width %d, want 42: %q", w, line)
		}
	}
	if !strings.Contains(box, "…") {
		t.Fatalf("long path not cut with …:\n%s", box)
	}
	if !strings.Contains(box, "—") {
		t.Fatalf("file with no folder shows no dash:\n%s", box)
	}
	if !strings.Contains(box, "just now") {
		t.Fatalf("future time not shown as just now:\n%s", box)
	}
	// A long typed query is cut, not wrapped.
	m, _ = m.Update(runeKey(strings.Repeat("q", 60)))
	for _, line := range strings.Split(ansi.Strip(m.Box()), "\n") {
		if w := ansi.StringWidth(line); w != 42 {
			t.Fatalf("query row width %d, want 42", w)
		}
	}
}

func TestFormatRelative(t *testing.T) {
	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		ago  time.Duration
		want string
	}{
		{-time.Minute, "just now"},
		{10 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{3 * time.Hour, "3h ago"},
		{2 * 24 * time.Hour, "2d ago"},
		{30 * 24 * time.Hour, "Feb 18"},
	} {
		if got := formatRelative(now.Add(-c.ago), now); got != c.want {
			t.Errorf("formatRelative(-%v) = %q, want %q", c.ago, got, c.want)
		}
	}
	if matchCountText(0) != "no matches" || matchCountText(1) != "1 match" || matchCountText(12) != "12 matches" {
		t.Fatal("match count text")
	}
	if itoa(0) != "0" || itoa(-3) != "0" || itoa(907) != "907" {
		t.Fatal("itoa")
	}
}

func TestTruncHelpers(t *testing.T) {
	if truncRight("abc", 0) != "" || truncLeft("abc", 0) != "" {
		t.Fatal("zero width must give empty")
	}
	if got := truncRight("abcdef", 4); got != "abc…" {
		t.Fatalf("truncRight = %q", got)
	}
	if got := truncLeft("/a/b/cdef", 5); got != "…cdef" {
		t.Fatalf("truncLeft = %q", got)
	}
	// Wide runes: the tail never goes over the width.
	if got := truncLeft("日本語のファイル", 5); ansi.StringWidth(got) > 5 || !strings.HasPrefix(got, "…") {
		t.Fatalf("truncLeft wide = %q", got)
	}
}

func TestMouseFragmentHelpers(t *testing.T) {
	if isMouseCodeRunes(nil) {
		t.Fatal("empty runes are not a mouse code")
	}
	if isMouseFragmentEvent([]rune("[a")) || isMouseFragmentEvent([]rune("ab")) {
		t.Fatal("plain text is not a mouse fragment")
	}
	if !isMouseFragmentEvent([]rune("[<35;70;19M")) {
		t.Fatal("SGR fragment not caught")
	}
}
