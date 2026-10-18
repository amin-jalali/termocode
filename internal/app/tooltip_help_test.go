package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/theme"
)

// maxAllocsTooltipMotion bounds one motion Update while a label with a
// help line shows and the pointer moves inside it (measured 5).
const maxAllocsTooltipMotion = 7

// tipTarget is the hit target id in m's current state.
func tipTarget(t testing.TB, m Model, id string) hitTarget {
	t.Helper()
	for _, h := range m.hitTargets() {
		if h.ID == id {
			return h
		}
	}
	t.Fatalf("no hit target %q", id)
	return hitTarget{}
}

// showTipOn rests the pointer on target id and shows its label.
func showTipOn(t testing.TB, m Model, id string) Model {
	t.Helper()
	h := tipTarget(t, m, id)
	m = hoverAt(m, h.X+h.W/2, h.Y+h.H/2)
	m.tip.moved = time.Now().Add(-time.Second)
	m.onTooltipTick(tooltipTickMsg{m.tip.gen})
	if !m.tip.shown {
		t.Fatalf("no label on %s", id)
	}
	return m
}

func motionMsg(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Type: tea.MouseMotion, Action: tea.MouseActionMotion}
}

func pressMsg(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

// screenRow is the plain text of row y of the frame.
func screenRow(m Model, y int) string {
	rows := strings.Split(m.View(), "\n")
	if y < 0 || y >= len(rows) {
		return ""
	}
	return ansi.Strip(rows[y])
}

// Spec: every help topic a hover label or a focused item points at is a
// real page, heading and (when given) text of the built-in guide, so a
// renamed heading fails the build, not the user.
func TestHelpTopicsResolve(t *testing.T) {
	var topics []string
	for id, s := range tooltipLabels {
		if s.help == "" {
			continue
		}
		if _, err := help.Parse(s.help); err != nil {
			t.Errorf("%s: %v", id, err)
		}
		topics = append(topics, s.help)
	}
	for name, topic := range activityHelp {
		if topic == "" {
			t.Errorf("activity %s: empty topic", name)
		}
		topics = append(topics, topic)
	}
	for _, err := range help.CheckAll(topics) {
		t.Error(err)
	}
	// The check itself must catch a bad anchor.
	if errs := help.CheckAll([]string{"guides/ai-use.md#no-such-heading"}); len(errs) != 1 {
		t.Errorf("a bad anchor passed the check")
	}
	// Labels with nothing to point at stay without a help line.
	none := map[string]bool{"overflow:glyph": true, "panel:tab-close:": true,
		"status:indent": true, "status:encoding": true, "status:pos": true}
	for id, s := range tooltipLabels {
		if none[id] != (s.help == "") {
			t.Errorf("%s: help = %q, want a topic: %v", id, s.help, !none[id])
		}
	}
	for _, name := range []string{"Explorer", "Source Control", "Run", "Testing", "Services"} {
		if activityHelp[name] == "" {
			t.Errorf("activity %s has no help topic", name)
		}
	}
}

// Spec: a label whose button has a guide section gets its own last line
// "ⓘ More info" ("(i) More info" with ASCII icons), as wide as the
// label; a label without one stays a single row.
func TestTooltipHelpLine(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	prev := theme.CurrentIconMode
	defer func() { theme.CurrentIconMode = prev }()
	theme.CurrentIconMode = theme.IconModeUnicode
	m := showTipOn(t, hoverModel(t), "activity:Explorer")
	if m.tip.help != "guides/editing.md#open-files" {
		t.Errorf("help = %q", m.tip.help)
	}
	if got := screenRow(m, m.tip.y); !strings.Contains(got, "Explorer") {
		t.Errorf("label row %d = %q", m.tip.y, got)
	}
	row := screenRow(m, m.tip.y+1)
	w := ansi.StringWidth(m.tip.text)
	if got := ansi.Cut(row, m.tip.x, m.tip.x+w); strings.TrimSpace(got) != "ⓘ More info" {
		t.Errorf("help row = %q (full %q)", got, row)
	}

	m = showTipOn(t, hoverModel(t), "status:pos")
	if m.tip.help != "" || m.tip.rows() != 1 {
		t.Errorf("status:pos: help %q rows %d, want none", m.tip.help, m.tip.rows())
	}
	if strings.Contains(m.View(), "More info") {
		t.Errorf("status:pos label has a help line")
	}

	theme.CurrentIconMode = theme.IconModeASCII
	m = showTipOn(t, hoverModel(t), "activity:Explorer")
	if got := screenRow(m, m.tip.y+1); !strings.Contains(got, "(i) More info") {
		t.Errorf("ASCII help row = %q", got)
	}
}

// Spec: the pointer may move from the button onto the label (to reach
// the help line); leaving both keeps the label until the tick in flight,
// which then hides it. A label without a help line hides at once.
func TestTooltipHelpGrace(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := showTipOn(t, hoverModel(t), "activity:Explorer")
	if cmd := m.tooltipOnInput(motionMsg(m.tip.x+2, m.tip.y+1)); cmd != nil || !m.tip.shown {
		t.Fatalf("move onto the help line: cmd=%v shown=%v", cmd != nil, m.tip.shown)
	}
	m = hoverAt(m, m.tip.x+2, m.tip.y+1)
	if !m.tip.shown {
		t.Fatal("label hid on the help line")
	}
	// Away from both: still shown, one tick pending.
	away := motionMsg(m.w/2, m.h/2)
	cmd := m.tooltipOnInput(away)
	if !m.tip.shown || !m.tip.pending || cmd == nil {
		t.Fatalf("leave: shown=%v pending=%v cmd=%v", m.tip.shown, m.tip.pending, cmd != nil)
	}
	if again := m.tooltipOnInput(away); again != nil {
		t.Errorf("a second tick was scheduled")
	}
	m.hoverX, m.hoverY = away.X, away.Y
	m.tip.moved = time.Now().Add(-time.Second)
	m.onTooltipTick(tooltipTickMsg{m.tip.gen})
	if m.tip.shown {
		t.Errorf("tick left the label up with the pointer outside")
	}

	m = showTipOn(t, hoverModel(t), "status:pos")
	if m.tooltipOnInput(motionMsg(m.w/2, m.h/2)); m.tip.shown {
		t.Errorf("label without help line stays after leaving")
	}
}

// Spec: a click on the help line opens the guide at the label's topic,
// and the click goes nowhere else; any other click hides the label.
func TestTooltipHelpClick(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := showTipOn(t, hoverModel(t), "activity:Explorer")
	hx, hy := m.tip.x+1, m.tip.y+1
	if got := m.probeClick(hx, hy); got != "tip:help" {
		t.Errorf("probeClick on the help line = %q, want tip:help", got)
	}
	found := false
	for _, h := range m.hitTargets() {
		if h.ID == "tip:help" {
			found = h.X == m.tip.x && h.Y == hy && h.W == ansi.StringWidth(m.tip.text)
		}
	}
	if !found {
		t.Errorf("no tip:help hit target on the help line")
	}
	if name, _ := m.tooltipLabel("tip:help"); name != "" {
		t.Errorf("tip:help has its own label %q", name)
	}
	cmd, ok := m.tooltipHelpPress(pressMsg(hx, hy))
	if !ok || cmd == nil {
		t.Fatalf("press on help line: ok=%v cmd=%v", ok, cmd != nil)
	}
	if m.tip.shown {
		t.Errorf("label still shown after the click")
	}
	msg, isPreview := cmd().(PreviewMsg)
	if !isPreview {
		t.Fatalf("cmd gave %T, want PreviewMsg", msg)
	}
	nm, _ := m.Update(msg)
	m = nm.(Model)
	if !m.helpActive() || m.help.cur.Anchor != "open-files" {
		t.Errorf("guide not open at open-files: active=%v cur=%+v", m.helpActive(), m.help.cur)
	}

	// Through Update: the press is consumed by the help line.
	m = showTipOn(t, hoverModel(t), "activity:Explorer")
	nm, cmd = m.Update(pressMsg(m.tip.x+1, m.tip.y+1))
	m = nm.(Model)
	if m.tip.shown || cmd == nil {
		t.Errorf("Update press: shown=%v cmd=%v", m.tip.shown, cmd != nil)
	}
	// A press elsewhere is not the help line.
	m = showTipOn(t, hoverModel(t), "activity:Explorer")
	if _, ok := m.tooltipHelpPress(pressMsg(m.w/2, m.h/2)); ok {
		t.Errorf("a press away from the help line opened help")
	}
}

// Spec: the help line takes the primary colours while the pointer is on
// it.
func TestTooltipHelpHover(t *testing.T) {
	perfColour(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := showTipOn(t, hoverModel(t), "activity:Explorer")
	off := strings.Split(m.View(), "\n")[m.tip.y+1]
	m = hoverAt(m, m.tip.x+2, m.tip.y+1)
	on := strings.Split(m.View(), "\n")[m.tip.y+1]
	if on == off || ansi.Strip(on) != ansi.Strip(off) {
		t.Errorf("hover does not restyle the help line:\noff %q\non  %q", off, on)
	}
}

// Perf guard: a motion inside a shown label with a help line stays a
// cheap check (no tick, few allocations).
func TestPerfGuardTooltipMotion(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := showTipOn(t, hoverModel(t), "activity:Explorer")
	x, y := m.tip.x+2, m.tip.y+1
	m = hoverAt(m, x, y)
	n := testing.AllocsPerRun(50, func() { m = hoverAt(m, x, y) })
	if !m.tip.shown || m.tip.pending {
		t.Errorf("motion inside the label: shown=%v pending=%v", m.tip.shown, m.tip.pending)
	}
	if n > maxAllocsTooltipMotion {
		t.Errorf("motion over the label: %.0f allocs, limit %d", n, maxAllocsTooltipMotion)
	} else {
		t.Logf("motion over the label: %.0f allocs (limit %d)", n, maxAllocsTooltipMotion)
	}
}
