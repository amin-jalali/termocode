package findbar

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
)

// TestMain forces lipgloss into TrueColor mode for the duration of the
// tests in this package. By default, lipgloss/termenv detect the test
// runner's stdout as a non-TTY and strip ANSI colour escapes.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}

func TestNewIsEmpty(t *testing.T) {
	m := New()
	if m.Query() != "" {
		t.Fatalf("new query should be empty")
	}
}

func TestUpdateTypingFiresSearch(t *testing.T) {
	m := New()
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")})
	if m.Query() != "hi" {
		t.Fatalf("query want 'hi', got %q", m.Query())
	}
	if cmd == nil {
		t.Fatal("expected SearchMsg cmd")
	}
	msg, ok := cmd().(SearchMsg)
	if !ok {
		t.Fatalf("want SearchMsg, got %T", cmd())
	}
	if msg.Query != "hi" {
		t.Fatalf("SearchMsg.Query want 'hi', got %q", msg.Query)
	}
}

func TestUpdateBackspaceTrimsAndSearches(t *testing.T) {
	m := New()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ab")})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.Query() != "a" {
		t.Fatalf("query after backspace: want 'a', got %q", m.Query())
	}
	if cmd == nil {
		t.Fatal("backspace should fire SearchMsg")
	}
}

func TestUpdateBackspaceOnEmptyIsNoop(t *testing.T) {
	m := New()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if cmd != nil {
		t.Fatal("backspace on empty input must not emit cmd")
	}
}

func TestUpdateSpaceFiresSearch(t *testing.T) {
	m := New()
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if m.Query() != " " {
		t.Fatalf("space input want ' ', got %q", m.Query())
	}
	if cmd == nil {
		t.Fatal("space must emit SearchMsg")
	}
}

func TestUpdateEnterEmitsFindNext(t *testing.T) {
	m := New()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter expected to emit cmd")
	}
	if _, ok := cmd().(FindNextMsg); !ok {
		t.Fatalf("want FindNextMsg, got %T", cmd())
	}
}

func TestUpdateShiftTabEmitsFindPrev(t *testing.T) {
	m := New()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd == nil {
		t.Fatal("Shift+Tab expected to emit cmd")
	}
	if _, ok := cmd().(FindPrevMsg); !ok {
		t.Fatalf("want FindPrevMsg, got %T", cmd())
	}
}

func TestUpdateEscEmitsClose(t *testing.T) {
	m := New()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc expected to emit cmd")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Fatalf("want CloseMsg, got %T", cmd())
	}
}

func TestUpdateNonKeyMsgIsNoop(t *testing.T) {
	m := New()
	_, cmd := m.Update(struct{}{})
	if cmd != nil {
		t.Fatal("non-key msg must not emit cmd")
	}
}

func TestViewEmptyWithoutWidth(t *testing.T) {
	m := New()
	if v := m.View(); v != "" {
		t.Fatalf("zero width view want empty, got %q", v)
	}
}

func TestViewIncludesLabelAndQuery(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("foo")})
	out := m.View()
	// Collapsed find row carries a right-pointing chevron on the far left.
	if !strings.Contains(out, "▸") {
		t.Fatalf("collapsed chevron glyph missing in view: %q", out)
	}
	if !strings.Contains(out, "foo") {
		t.Fatalf("query 'foo' missing in view")
	}
}

func TestViewShowsPlaceholderWhenEmpty(t *testing.T) {
	m := New()
	m.SetWidth(80)
	out := m.View()
	if !strings.Contains(out, "Find") {
		t.Fatalf("placeholder 'Find' missing on empty input")
	}
}

func TestPanelHeightIsThree(t *testing.T) {
	m := New()
	m.SetWidth(80)
	if got := m.PanelHeight(); got != 3 {
		t.Fatalf("PanelHeight want 3, got %d", got)
	}
	out := m.View()
	if got := strings.Count(out, "\n") + 1; got != 3 {
		t.Fatalf("rendered rows want 3, got %d", got)
	}
}

func TestSetMatchCountRendersInView(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("foo")})
	m.SetMatchCount("3/12")
	out := m.View()
	if !strings.Contains(out, "3/12") {
		t.Fatalf("match count '3/12' missing in view: %q", out)
	}
}

func TestSetMatchCountZeroRenders(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m.SetMatchCount("0")
	out := m.View()
	if !strings.Contains(out, "0") {
		t.Fatalf("zero count missing in view: %q", out)
	}
}

func TestSetMatchCountEmptyRendersNoDigits(t *testing.T) {
	m := New()
	m.SetWidth(80)
	out := m.View()
	if strings.Contains(out, "/") {
		t.Fatalf("empty match count must not render '/': %q", out)
	}
}

func TestResetClearsMatchCount(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m.SetMatchCount("3/12")
	m.Reset()
	out := m.View()
	if strings.Contains(out, "3/12") {
		t.Fatalf("Reset should clear match count, got view: %q", out)
	}
}

func TestSetAnchorStoredInBounds(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m.SetAnchor(40, 5)
	x, y, w, h := m.Bounds()
	if x != 40 || y != 5 {
		t.Fatalf("Bounds origin want (40,5), got (%d,%d)", x, y)
	}
	if w != m.PanelWidth() || h != m.PanelHeight() {
		t.Fatalf("Bounds size want (%d,%d), got (%d,%d)", m.PanelWidth(), m.PanelHeight(), w, h)
	}
}

func TestHandleMouseClickOutsidePanelIsNoop(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m.SetAnchor(40, 5)
	_, cmd := m.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonLeft)
	if cmd != nil {
		t.Fatalf("click outside panel must return no cmd")
	}
}

func TestHandleMouseClickPrevButtonEmitsFindPrev(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m.SetAnchor(40, 5)
	_, _, prev, _, _ := m.buttonColumns()
	_, cmd := m.HandleMouse(40+prev, 5+1, tea.MouseActionPress, tea.MouseButtonLeft)
	if cmd == nil {
		t.Fatal("click on ↑ must emit cmd")
	}
	if _, ok := cmd().(FindPrevMsg); !ok {
		t.Fatalf("want FindPrevMsg, got %T", cmd())
	}
}

func TestHandleMouseClickNextButtonEmitsFindNext(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m.SetAnchor(40, 5)
	_, _, _, next, _ := m.buttonColumns()
	_, cmd := m.HandleMouse(40+next, 5+1, tea.MouseActionPress, tea.MouseButtonLeft)
	if cmd == nil {
		t.Fatal("click on ↓ must emit cmd")
	}
	if _, ok := cmd().(FindNextMsg); !ok {
		t.Fatalf("want FindNextMsg, got %T", cmd())
	}
}

func TestHandleMouseClickCloseButtonEmitsClose(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m.SetAnchor(40, 5)
	_, _, _, _, closeCol := m.buttonColumns()
	_, cmd := m.HandleMouse(40+closeCol, 5+1, tea.MouseActionPress, tea.MouseButtonLeft)
	if cmd == nil {
		t.Fatal("click on × must emit cmd")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Fatalf("want CloseMsg, got %T", cmd())
	}
}

func TestHandleMouseClickInsideButNotOnButtonIsNoop(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m.SetAnchor(40, 5)
	_, cmd := m.HandleMouse(40, 5, tea.MouseActionPress, tea.MouseButtonLeft)
	if cmd != nil {
		t.Fatalf("click on top border must not emit cmd, got %T", cmd())
	}
}

func TestHandleMouseNonLeftPressInsidePanelIsNoop(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m.SetAnchor(40, 5)
	_, _, prev, _, _ := m.buttonColumns()
	_, cmd := m.HandleMouse(40+prev, 5+1, tea.MouseActionPress, tea.MouseButtonRight)
	if cmd != nil {
		t.Fatalf("right-click on ↑ must not emit cmd, got %T", cmd())
	}
	_, _, _, _, closeCol := m.buttonColumns()
	_, cmd = m.HandleMouse(40+closeCol, 5+1, tea.MouseActionMotion, tea.MouseButtonNone)
	if cmd != nil {
		t.Fatalf("motion on × must not emit cmd, got %T", cmd())
	}
}

func TestButtonColumnsAscending(t *testing.T) {
	m := New()
	m.SetWidth(80)
	caseCol, regexCol, prev, next, closeCol := m.buttonColumns()
	if caseCol <= 0 || regexCol <= caseCol || prev <= regexCol || next <= prev || closeCol <= next {
		t.Fatalf("expected ascending button columns, got case=%d regex=%d prev=%d next=%d close=%d",
			caseCol, regexCol, prev, next, closeCol)
	}
}

// TestButtonColumnsMatchRenderedGlyphs is a layout-drift guard: it strips
// ANSI from the rendered input row and verifies that ↑ / ↓ / × actually
// land at the cells reported by buttonColumns().
func TestButtonColumnsMatchRenderedGlyphs(t *testing.T) {
	m := New()
	m.SetWidth(80)
	rows := strings.Split(m.View(), "\n")
	if len(rows) < 2 {
		t.Fatalf("not enough rows in rendered panel: %d", len(rows))
	}
	visible := visibleCells(rows[1])
	caseCol, regexCol, prev, next, closeCol := m.buttonColumns()
	for _, tc := range []struct {
		name  string
		col   int
		glyph string
	}{
		{"case", caseCol, "A"},
		{"regex", regexCol, "."},
		{"prev", prev, "↑"},
		{"next", next, "↓"},
		{"close", closeCol, "×"},
	} {
		got := cellAt(visible, tc.col)
		if got != tc.glyph {
			t.Fatalf("%s: cell %d want %q, got %q (row=%q)", tc.name, tc.col, tc.glyph, got, visible)
		}
	}
}

func TestNewToggleDefaultsAreOff(t *testing.T) {
	m := New()
	if m.CaseSensitive() {
		t.Fatal("default case-sensitive should be off")
	}
	if m.UseRegex() {
		t.Fatal("default regex should be off")
	}
}

func TestHandleMouseClickCaseToggle(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m.SetAnchor(40, 5)
	caseCol, _, _, _, _ := m.buttonColumns()
	// Click the first cell of the "Aa" glyph.
	m, cmd := m.HandleMouse(40+caseCol, 5+1, tea.MouseActionPress, tea.MouseButtonLeft)
	if !m.CaseSensitive() {
		t.Fatal("case toggle should be on after click")
	}
	if cmd == nil {
		t.Fatal("toggle should re-fire SearchMsg so the host can re-run search")
	}
	if _, ok := cmd().(SearchMsg); !ok {
		t.Fatalf("want SearchMsg after toggle, got %T", cmd())
	}
	// Click again on the SECOND cell of "Aa" — should still toggle.
	m, _ = m.HandleMouse(40+caseCol+1, 5+1, tea.MouseActionPress, tea.MouseButtonLeft)
	if m.CaseSensitive() {
		t.Fatal("case toggle should be off after second click")
	}
}

func TestHandleMouseClickRegexToggle(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m.SetAnchor(40, 5)
	_, regexCol, _, _, _ := m.buttonColumns()
	m, cmd := m.HandleMouse(40+regexCol, 5+1, tea.MouseActionPress, tea.MouseButtonLeft)
	if !m.UseRegex() {
		t.Fatal("regex toggle should be on after click")
	}
	if cmd == nil {
		t.Fatal("toggle should re-fire SearchMsg")
	}
	if _, ok := cmd().(SearchMsg); !ok {
		t.Fatalf("want SearchMsg after toggle, got %T", cmd())
	}
	m, _ = m.HandleMouse(40+regexCol+1, 5+1, tea.MouseActionPress, tea.MouseButtonLeft)
	if m.UseRegex() {
		t.Fatal("regex toggle should be off after second click")
	}
}

// visibleCells strips ANSI CSI sequences from a rendered row.
func visibleCells(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		if c == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// cellAt returns the rune (as a string) at visible cell index col of s.
// Accounts for east-asian / emoji wide characters that occupy 2 cells.
func cellAt(s string, col int) string {
	idx := 0
	runes := []rune(s)
	for _, r := range runes {
		if idx == col {
			return string(r)
		}
		w := runewidth.RuneWidth(r)
		if w == 0 {
			w = 1
		}
		idx += w
	}
	return ""
}

// ── Replace-mode / chevron / whole-word / new-msg tests ────────────────────

func TestPanelHeightExpandsInReplaceMode(t *testing.T) {
	m := New()
	m.SetWidth(80)
	if got := m.PanelHeight(); got != 3 {
		t.Fatalf("PanelHeight collapsed want 3, got %d", got)
	}
	m.EnableReplace(true)
	// Expanded panel = top border + find + blank + replace + bottom border = 5.
	if got := m.PanelHeight(); got != 5 {
		t.Fatalf("PanelHeight expanded want 5, got %d", got)
	}
	out := m.View()
	if got := strings.Count(out, "\n") + 1; got != 5 {
		t.Fatalf("rendered rows want 5, got %d", got)
	}
}

func TestEnableReplaceTogglesReplaceMode(t *testing.T) {
	m := New()
	m.SetWidth(80)
	if m.ReplaceMode() {
		t.Fatal("default replace mode should be off")
	}
	m.EnableReplace(true)
	if !m.ReplaceMode() {
		t.Fatal("EnableReplace(true) should turn replace on")
	}
	m.EnableReplace(false)
	if m.ReplaceMode() {
		t.Fatal("EnableReplace(false) should turn replace off")
	}
}

func TestChevronGlyphReflectsReplaceMode(t *testing.T) {
	m := New()
	m.SetWidth(80)
	if !strings.Contains(m.View(), "▸") {
		t.Fatal("collapsed view should show right-pointing chevron")
	}
	m.EnableReplace(true)
	if !strings.Contains(m.View(), "▾") {
		t.Fatal("expanded view should show down-pointing chevron")
	}
}

func TestChevronClickTogglesReplaceMode(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m.SetAnchor(40, 5)
	chev, _, _, _, _, _, _, _ := m.buttonColumnsFind()
	if chev < 0 {
		t.Fatal("buttonColumnsFind returned -1 chevron column")
	}
	m, _ = m.HandleMouse(40+chev, 5+1, tea.MouseActionPress, tea.MouseButtonLeft)
	if !m.ReplaceMode() {
		t.Fatal("click on chevron should enable replace mode")
	}
	m, _ = m.HandleMouse(40+chev, 5+1, tea.MouseActionPress, tea.MouseButtonLeft)
	if m.ReplaceMode() {
		t.Fatal("second chevron click should disable replace mode")
	}
}

func TestWholeWordToggleViaMouse(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m.SetAnchor(40, 5)
	_, _, word, _, _, _, _, _ := m.buttonColumnsFind()
	if word < 0 {
		t.Fatal("buttonColumnsFind returned -1 word column")
	}
	if m.WholeWord() {
		t.Fatal("default whole-word should be off")
	}
	m, cmd := m.HandleMouse(40+word, 5+1, tea.MouseActionPress, tea.MouseButtonLeft)
	if !m.WholeWord() {
		t.Fatal("click on whole-word glyph should enable")
	}
	if cmd == nil {
		t.Fatal("whole-word toggle should re-fire SearchMsg")
	}
	if _, ok := cmd().(SearchMsg); !ok {
		t.Fatalf("want SearchMsg after whole-word toggle, got %T", cmd())
	}
}

func TestTabTogglesFocusInReplaceMode(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m.EnableReplace(true)
	// EnableReplace puts focus on Replace; Tab should hop back to Find.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	// Type into the focused field — should land in m.input (Find).
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if m.Query() != "q" {
		t.Fatalf("after Tab+typing, find input want 'q', got %q", m.Query())
	}
	// Tab again → focus to Replace; typing should fill m.replace.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if m.ReplaceValue() != "r" {
		t.Fatalf("after Tab to Replace+typing, replace want 'r', got %q", m.ReplaceValue())
	}
}

func TestTypingInReplaceFieldFillsReplace(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m.EnableReplace(true) // focus is on Replace
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")})
	if m.ReplaceValue() != "hi" {
		t.Fatalf("replace want 'hi', got %q", m.ReplaceValue())
	}
	if m.Query() != "" {
		t.Fatalf("find input must remain empty when typing in replace, got %q", m.Query())
	}
}

func TestEnterInReplaceFiresReplaceMsg(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("foo")})
	m.EnableReplace(true)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bar")})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter on replace input should emit cmd")
	}
	msg := cmd()
	rm, ok := msg.(ReplaceMsg)
	if !ok {
		t.Fatalf("want ReplaceMsg, got %T", msg)
	}
	if rm.Find != "foo" || rm.Replace != "bar" {
		t.Fatalf("ReplaceMsg fields want {foo,bar}, got %+v", rm)
	}
}

func TestAltEnterInReplaceFiresReplaceAllMsg(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("foo")})
	m.EnableReplace(true)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bar")})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if cmd == nil {
		t.Fatal("Alt+Enter on replace input should emit cmd")
	}
	rm, ok := cmd().(ReplaceAllMsg)
	if !ok {
		t.Fatalf("want ReplaceAllMsg, got %T", cmd())
	}
	if rm.Find != "foo" || rm.Replace != "bar" {
		t.Fatalf("ReplaceAllMsg fields want {foo,bar}, got %+v", rm)
	}
}

func TestReplaceOneIconClickEmitsReplaceMsg(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m.SetAnchor(40, 5)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("foo")})
	m.EnableReplace(true)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bar")})
	one, _ := m.buttonColumnsReplace()
	if one < 0 {
		t.Fatal("buttonColumnsReplace returned -1 in replace mode")
	}
	_, cmd := m.HandleMouse(40+one, 5+2, tea.MouseActionPress, tea.MouseButtonLeft)
	if cmd == nil {
		t.Fatal("click on replace-one icon should emit cmd")
	}
	rm, ok := cmd().(ReplaceMsg)
	if !ok {
		t.Fatalf("want ReplaceMsg, got %T", cmd())
	}
	if rm.Find != "foo" || rm.Replace != "bar" {
		t.Fatalf("ReplaceMsg fields want {foo,bar}, got %+v", rm)
	}
}

func TestReplaceAllIconClickEmitsReplaceAllMsg(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m.SetAnchor(40, 5)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("foo")})
	m.EnableReplace(true)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bar")})
	_, all := m.buttonColumnsReplace()
	if all < 0 {
		t.Fatal("buttonColumnsReplace returned -1 in replace mode")
	}
	_, cmd := m.HandleMouse(40+all, 5+2, tea.MouseActionPress, tea.MouseButtonLeft)
	if cmd == nil {
		t.Fatal("click on replace-all icon should emit cmd")
	}
	if _, ok := cmd().(ReplaceAllMsg); !ok {
		t.Fatalf("want ReplaceAllMsg, got %T", cmd())
	}
}

func TestResetClearsReplaceValue(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m.EnableReplace(true)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if m.ReplaceValue() != "x" {
		t.Fatalf("replace want 'x', got %q", m.ReplaceValue())
	}
	m.Reset()
	if m.ReplaceValue() != "" {
		t.Fatalf("Reset should clear replace value, got %q", m.ReplaceValue())
	}
}
