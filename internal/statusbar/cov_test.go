package statusbar

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func trueColor(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
}

func barOf(w int) Model {
	m := New()
	m.SetWidth(w)
	return m
}

func setIconMode(t *testing.T, mode theme.IconMode) {
	old := theme.CurrentIconMode
	theme.CurrentIconMode = mode
	t.Cleanup(func() { theme.CurrentIconMode = old })
}

// Every click-span helper says "nothing there" on a bar with no width.
func TestSpansAreEmptyWithoutWidth(t *testing.T) {
	m := New()
	s := State{Debug: true, Plan: "P", ShowTests: true, Err: "", Ext: []string{"x"}}
	if x0, x1, ok := m.LSPChipSpan(s); ok || x0 != 0 || x1 != 0 {
		t.Fatal("LSP span")
	}
	if _, _, ok := m.DiagSpan(s); ok {
		t.Fatal("diag span")
	}
	if _, _, ok := m.AIBadgeSpan(s); ok {
		t.Fatal("AI span")
	}
	if _, _, ok := m.TestsChipSpan(s); ok {
		t.Fatal("tests span")
	}
	if _, _, ok := m.PlanChipSpan(s); ok {
		t.Fatal("plan span")
	}
	if m.ItemSpans(s) != nil || m.ExtItemSpans(s) != nil {
		t.Fatal("item spans")
	}
}

// A second View of the same state at the same width comes from the cache
// and is identical.
func TestViewSecondCallIsCached(t *testing.T) {
	m := barOf(60)
	s := State{Project: "proj", Branch: "main", Line: 1, Col: 1}
	a, b := m.View(s), m.View(s)
	if a != b || a == "" {
		t.Fatal("cached view differs")
	}
}

// The DEBUG badge has a span only while it fits.
func TestDebugBadgeSpanDroppedWhenNarrow(t *testing.T) {
	if _, _, ok := barOf(100).DebugBadgeSpan(State{Debug: true, Line: 1, Col: 1}); !ok {
		t.Fatal("badge should show on a wide bar")
	}
	if _, _, ok := barOf(8).DebugBadgeSpan(State{Debug: true, Line: 1, Col: 1, Project: "a-long-project"}); ok {
		t.Fatal("badge should be dropped on a tiny bar")
	}
	if _, _, ok := barOf(100).DebugBadgeSpan(State{}); ok {
		t.Fatal("no session, no badge")
	}
}

// A bar narrower than its own padding still draws without panic and
// keeps its width.
func TestViewTinyWidths(t *testing.T) {
	for _, w := range []int{1, 2, 3, 5} {
		out := barOf(w).View(State{Project: "project", Branch: "main", Line: 12, Col: 3, Debug: true})
		if got := lipgloss.Width(out); got > w+2 {
			t.Fatalf("w=%d drew %d cells", w, got)
		}
	}
}

// Branch ahead/behind counts show after the branch name, in every icon mode.
func TestBranchTextAheadBehindAndGlyph(t *testing.T) {
	for _, mode := range []theme.IconMode{theme.IconModeNerdFont, theme.IconModeUnicode, theme.IconModeASCII} {
		setIconMode(t, mode)
		got := branchText(State{Branch: "main", Ahead: 2, Behind: 3})
		if !strings.Contains(got, "main ↑2 ↓3") {
			t.Fatalf("mode %v: %q", mode, got)
		}
	}
	setIconMode(t, theme.IconModeNerdFont)
	if glyph("N", "U") != "N" {
		t.Fatal("nerd glyph")
	}
	setIconMode(t, theme.IconModeUnicode)
	if glyph("N", "U") != "U" {
		t.Fatal("unicode glyph")
	}
	setIconMode(t, theme.IconModeASCII)
	if glyph("N", "U") != "" {
		t.Fatal("ascii glyph")
	}
}

// The branch is the first clickable item of the center when it fits.
func TestItemSpansBranchFirst(t *testing.T) {
	m := barOf(120)
	for _, sp := range m.ItemSpans(State{Project: "p", Branch: "main", Line: 1, Col: 1}) {
		if sp.ID == "branch" && sp.X1 > sp.X0 {
			return
		}
	}
	t.Fatal("no branch span")
}

// A long terminal label keeps its shell and the end of its folder; a
// label that fits stays; a long shell name is cut whole.
func TestShortenTermLabel(t *testing.T) {
	if got := shortenTermLabel("zsh · ~", 40); got != "zsh · ~" {
		t.Fatalf("fits: %q", got)
	}
	got := shortenTermLabel("zsh · /home/me/projects/deep/folder", 18)
	if !strings.HasPrefix(got, "zsh · …") || !strings.HasSuffix(got, "folder") || ansi.StringWidth(got) > 18 {
		t.Fatalf("folder: %q", got)
	}
	got = shortenTermLabel("averyverylongshellname · /dir", 12)
	if ansi.StringWidth(got) > 12 {
		t.Fatalf("shell: %q", got)
	}
}

// The right cluster shows the AI badge, the debug pill and a language
// icon (in an icon mode) when they are set.
func TestRightClusterAIDebugAndLangIcon(t *testing.T) {
	setIconMode(t, theme.IconModeUnicode)
	m := barOf(160)
	out := ansi.Strip(m.View(State{
		Path: "/p/main.go", Lang: "Go", Line: 1, Col: 1, Debug: true,
		AI: "model-x", AIState: AIStreaming, Project: "p",
	}))
	for _, want := range []string{"model-x", "DEBUG", "Go"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
	if !strings.Contains(ansi.Strip(renderAIBadge("m", AIStreaming)), "m …") {
		t.Fatal("streaming badge has no dots")
	}
	if !strings.Contains(ansi.Strip(renderAIBadge("m", AIUnconfigured)), "AI off") {
		t.Fatal("unconfigured badge")
	}
}

// A production database chip is drawn in the error colour: its render
// differs from the same chip for a normal database.
func TestDatabaseChipProd(t *testing.T) {
	trueColor(t)
	m := barOf(140)
	base := State{ShowDatabase: true, DatabaseName: "db", Line: 1, Col: 1}
	prod := base
	prod.DatabaseProd = true
	if m.View(base) == m.View(prod) {
		t.Fatal("a production database looks like any other")
	}
}

// A running test run shows the spinner; failed tests change the colour.
func TestTestsChipRunningAndFailed(t *testing.T) {
	trueColor(t)
	if !strings.HasPrefix(ansi.Strip(renderTestsChip(State{ShowTests: true, TestsRunning: true, TestsPassed: 1})), "◐") {
		t.Fatal("no spinner while running")
	}
	ok := renderTestsChip(State{ShowTests: true, TestsPassed: 2})
	bad := renderTestsChip(State{ShowTests: true, TestsPassed: 2, TestsFailed: 1})
	if ok == bad {
		t.Fatal("failed tests look like passing ones")
	}
}

// A stale services chip is dimmed.
func TestServicesChipStale(t *testing.T) {
	trueColor(t)
	fresh := renderServicesChip(State{ShowServices: true, ServicesUp: 1, ServicesTotal: 2})
	stale := renderServicesChip(State{ShowServices: true, ServicesUp: 1, ServicesTotal: 2, ServicesStale: true})
	if fresh == stale {
		t.Fatal("stale chip is not dimmed")
	}
}

// Center chips have no click span when the center cluster was dropped
// for lack of room, or when the chip is not shown.
func TestCenterChipSpansWhenCenterDropped(t *testing.T) {
	m := barOf(30)
	s := State{
		Project: "a-rather-long-project-name", Branch: "feature/long-branch-name", Line: 1, Col: 1,
		ShowTests: true, TestsPassed: 1, WebOn: true, CI: CIFail, CIFailed: 1, Agents: 1,
		Plan: "✦ Claude 62%", PlanShort: "✦62%",
	}
	if _, _, ok := m.TestsChipSpan(s); ok {
		t.Fatal("tests span on a narrow bar")
	}
	if _, _, ok := m.WebChipSpan(s); ok {
		t.Fatal("web span on a narrow bar")
	}
	if _, _, ok := m.CIChipSpan(s); ok {
		t.Fatal("CI span on a narrow bar")
	}
	if _, _, ok := m.PlanChipSpan(s); ok {
		t.Fatal("plan span on a narrow bar")
	}
	empty := State{Line: 1, Col: 1}
	if _, _, ok := barOf(80).AgentsChipSpan(empty); ok {
		t.Fatal("agents span without a chip")
	}
	if len(barOf(30).ExtItemSpans(State{Ext: []string{"x"}, Project: "a-rather-long-project-name", Branch: "feature/long-branch-name"})) != 0 {
		t.Fatal("ext spans on a narrow bar")
	}
}

// With the terminal focused the right side shows the AI badge and the
// DEBUG pill next to the terminal label, not the editor facts.
func TestTerminalFocusedRightSide(t *testing.T) {
	out := ansi.Strip(barOf(140).View(State{
		TermFocused: true, TermLabel: "zsh · ~/p", AI: "model-x", AIState: AIIdle, Debug: true, Update: "v9.9.9",
	}))
	for _, want := range []string{"zsh · ~/p", "model-x", "DEBUG", "v9.9.9"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

// On a bar too small even for the short agents chip there is no span.
func TestAgentsChipSpanTinyBar(t *testing.T) {
	if _, _, ok := barOf(3).AgentsChipSpan(State{Agents: 1, Project: "project", Line: 1, Col: 1}); ok {
		t.Fatal("agents span on a 3-cell bar")
	}
}
