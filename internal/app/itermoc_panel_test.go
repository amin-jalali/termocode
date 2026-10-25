package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// imRowsOfKind counts the rows of a kind.
func imRowsOfKind(m *Model, kind imRowKind) int {
	n := 0
	for _, r := range m.itermocS().rows() {
		if r.kind == kind {
			n++
		}
	}
	return n
}

// Spec: "/" starts the inline filter; every key narrows the rows to the
// ones that match (sections without a match go), Enter keeps it, Esc clears.
func TestItermocFilter(t *testing.T) {
	imIcons(t, theme.IconModeUnicode)
	m := itermocTestModel(t, 70, 40)
	all := imRowsOfKind(m, imRowProfile)
	imPress(m, "/")
	if !m.itermoc.typing {
		t.Fatal("/ did not start the filter")
	}
	for _, r := range "hous" {
		imPress(m, string(r))
	}
	f := imFrame(m)
	if !strings.Contains(f, "/hous") {
		t.Errorf("the filter is not in the header:\n%s", f)
	}
	if got := imRowsOfKind(m, imRowProfile); got != 1 {
		t.Errorf("filter hous keeps %d of %d profiles", got, all)
	}
	for _, gone := range []string{"local-only", "MODELS", "SERVICES", "[New profile"} {
		if strings.Contains(f, gone) {
			t.Errorf("%q still shown with the filter on:\n%s", gone, f)
		}
	}
	imPress(m, "enter") // keep it, give the keys back
	if m.itermoc.typing || m.itermoc.filter != "hous" {
		t.Fatalf("Enter: typing=%v filter=%q", m.itermoc.typing, m.itermoc.filter)
	}
	imPress(m, "j") // a key again, not text
	if m.itermoc.filter != "hous" {
		t.Errorf("a key after Enter changed the filter: %q", m.itermoc.filter)
	}
	imPress(m, "esc")
	if m.itermoc.filter != "" || imRowsOfKind(m, imRowProfile) != all {
		t.Errorf("Esc did not clear the filter: %q", m.itermoc.filter)
	}
	if m.focus != FocusExplorer {
		t.Error("the first Esc left the view; it should only clear the filter")
	}
	imPress(m, "esc")
	if m.focus != FocusEditor {
		t.Error("Esc without a filter did not give the focus back")
	}
}

// Spec: a filter with no match says so and how to get the rows back.
func TestItermocFilterNoMatch(t *testing.T) {
	m := itermocTestModel(t, 70, 40)
	imPress(m, "/")
	for _, r := range "zzzz" {
		imPress(m, string(r))
	}
	if f := imFrame(m); !strings.Contains(f, `No rows match "zzzz"`) || !strings.Contains(f, "Esc clears") {
		t.Errorf("no empty filter state:\n%s", f)
	}
}

// Spec: "?" opens the key card with the keys of the view and a guide button.
func TestItermocQuestionMarkShowsKeyCard(t *testing.T) {
	m := itermocTestModel(t, 70, 40)
	imPress(m, "?")
	if !m.confirmOpen || m.confirmKind != confirmKindKeyCard {
		t.Fatal("no key card")
	}
	card := ansi.Strip(m.confirm.View())
	for _, want := range []string{"Itermoc keys", "Enter", "actions of the row", "filter by name", "Open guide"} {
		if !strings.Contains(card, want) {
			t.Errorf("no %q in\n%s", want, card)
		}
	}
	if m.keyCardGuide.Page != itermocGuidePage {
		t.Errorf("guide page %q", m.keyCardGuide.Page)
	}
}

// Spec: Space and Enter on a header fold the section; the folded header
// keeps the number that matters; Tab jumps from header to header.
func TestItermocFoldAndTab(t *testing.T) {
	imIcons(t, theme.IconModeUnicode)
	m := itermocTestModel(t, 70, 40)
	for i, r := range m.itermoc.rows() {
		if r.kind == imRowSection && r.sec == imSecProfiles {
			m.itermoc.cursor = i
		}
	}
	imPress(m, " ")
	if !m.itermoc.collapse[imSecProfiles] {
		t.Fatal("Space did not fold PROFILES")
	}
	f := imFrame(m)
	if strings.Contains(f, "local-only") || !strings.Contains(f, "PROFILES ▸") || !strings.Contains(f, "1 !") {
		t.Errorf("folded header:\n%s", f)
	}
	imPress(m, "enter")
	if m.itermoc.collapse[imSecProfiles] {
		t.Error("Enter on a header did not open it again")
	}
	imPress(m, "tab")
	if r, _ := m.itermoc.current(); r.kind != imRowSection || r.sec != imSecModels {
		t.Errorf("Tab went to %+v", r)
	}
	imPress(m, "shift+tab")
	if r, _ := m.itermoc.current(); r.sec != imSecProfiles {
		t.Errorf("Shift+Tab went to %+v", r)
	}
}

// Spec: the cursor skips the blank lines and notes; j / k and the arrows move
// between the rows you can act on (the "+ new profile…" row is one).
func TestItermocCursorSkipsButtons(t *testing.T) {
	m := itermocTestModel(t, 70, 40)
	imCursorTo(t, m, imRowProfile, "house") // the last profile; the "+ new profile…" row follows
	imPress(m, "j")
	if r, _ := m.itermoc.current(); r.kind != imRowAdd || r.sec != imSecProfiles {
		t.Fatalf("after the last profile the cursor is on %+v", r)
	}
	imPress(m, "j")
	r, _ := m.itermoc.current()
	if !r.selectable() || r.kind != imRowSection || r.sec != imSecModels {
		t.Errorf("after the last profile the cursor is on %+v", r)
	}
	imPress(m, "k")
	imPress(m, "k")
	if r, _ := m.itermoc.current(); r.name != "house" {
		t.Errorf("k went to %+v", r)
	}
}

// Spec: a click on a section header folds it; a click on a row selects it
// and opens its actions; a click on the dim "+ new profile…" row runs it.
// The wheel moves the cursor.
func TestItermocMouse(t *testing.T) {
	imIcons(t, theme.IconModeUnicode)
	m := itermocTestModel(t, 70, 40)
	w := m.explorerWidth - 1
	find := func(id string) hitTarget {
		t.Helper()
		for _, h := range m.itermocHits(w) {
			if h.ID == id {
				return h
			}
		}
		t.Fatalf("no hit target %q in %+v", id, m.itermocHits(w))
		return hitTarget{}
	}
	click := func(h hitTarget) {
		nm, _ := m.handleItermocSidebarMouse(h.X+h.W/2, h.Y, tea.MouseLeft)
		*m = nm
	}
	// Labels are really drawn where the target says.
	view := m.renderItermocSidebar(w, m.h)
	for _, h := range m.itermocHits(w) {
		if h.Label != "" && !strings.Contains(cellText(view, h.Y, h.X, h.X+h.W), h.Label) {
			t.Errorf("%s: %q is not drawn at (%d,%d): %q", h.ID, h.Label, h.X, h.Y, cellText(view, h.Y, h.X, h.X+h.W))
		}
	}
	click(find("im:row:profiles/review"))
	if !m.pickerOpen || m.pickerKind != pickerKindItermocActions {
		t.Fatal("a click on a profile row did not open its actions")
	}
	if r, _ := m.itermoc.current(); r.name != "review" {
		t.Errorf("the click selected %+v", r)
	}
	m.pickerOpen = false
	click(find("im:row:profiles/+add"))
	if !m.promptOpen || m.promptKind != promptKindAIProfileNew {
		t.Error("the + new profile… row did not open the name prompt")
	}
	m.promptOpen = false
	click(find("im:sec:models"))
	if !m.itermoc.collapse[imSecModels] {
		t.Error("a click on a header did not fold it")
	}
	// The wheel.
	m.itermoc.cursor = 0
	m.itermoc.clamp()
	nm, _ := m.handleItermocSidebarMouse(3, 5, tea.MouseWheelDown)
	*m = nm
	if m.itermoc.cursor == 0 {
		t.Error("the wheel did not move the cursor")
	}
	if got := m.sidebarTargetAt(3, find("im:row:profiles/fast").Y); got != "im:row:profiles/fast" {
		t.Errorf("sidebarTargetAt = %q", got)
	}
}

// Spec: the targets are in the global hit map, and a probed click resolves
// to the same id (the click goes through the mouse routing).
func TestItermocHitTargetsAndProbe(t *testing.T) {
	m := itermocTestModel(t, 70, 40)
	m.activity.SetHeight(m.h)
	var row hitTarget
	for _, h := range m.hitTargets() {
		if h.ID == "im:row:profiles/fast" {
			row = h
		}
	}
	if row.ID == "" {
		t.Fatal("the row is not in hitTargets")
	}
	if got := m.probeClick(row.X+2, row.Y); got != row.ID {
		t.Errorf("probeClick = %q, want %q", got, row.ID)
	}
	if got := m.tooltipText(row.ID); !strings.Contains(got, "Profile actions") {
		t.Errorf("tooltip %q", got)
	}
	if tooltipHelp(row.ID) == "" || tooltipHelp("im:row:profiles/+add") == "" {
		t.Error("no guide topic for the view's targets")
	}
	if name, key := m.tooltipLabel("im:row:services/+add"); name != imText("connect…") || key != "Enter" {
		t.Errorf("add-row tooltip %q %q", name, key)
	}
}

// Spec: the activity bar item opens the view (the click path), the view's
// help topic is its guide section.
func TestItermocActivityAndHelp(t *testing.T) {
	m := itermocTestModel(t, 70, 40)
	m.activity.SetActive(activity.ViewFiles)
	nm, _ := m.Update(activity.SwitchMsg{View: activity.ViewItermoc})
	got := nm.(Model)
	if got.activity.Active() != activity.ViewItermoc || got.itermoc == nil {
		t.Fatal("SwitchMsg did not open the view")
	}
	if got.sidebarHelpTopic() != "guides/itermoc.md#the-view" {
		t.Errorf("help topic %q", got.sidebarHelpTopic())
	}
}

// Spec: the view follows the state: a service added to mcp.json shows after
// R, and the cursor stays on the row it was on.
func TestItermocRefreshKeepsCursor(t *testing.T) {
	m := itermocTestModel(t, 70, 40)
	imCursorTo(t, m, imRowProfile, "fast")
	writeUserMCP(t, `{"mcpServers":{"notes":{"command":"notes-mcp"}}}`)
	imPress(m, "R")
	if _, ok := m.itermoc.snap.service("notes"); !ok {
		t.Error("R did not read mcp.json again")
	}
	if r, _ := m.itermoc.current(); r.name != "fast" {
		t.Errorf("the cursor moved to %+v", r)
	}
}

// Spec: a short window opens one section at a time (the profiles) and the
// folded headers keep their number.
func TestItermocShortWindowFoldsMostSections(t *testing.T) {
	imIcons(t, theme.IconModeUnicode)
	m := itermocTestModel(t, 29, 24)
	f := imFrame(m)
	if !strings.Contains(f, "PROFILES") || strings.Contains(f, "PROFILES ▸") || !strings.Contains(f, "MODELS ▸") || !strings.Contains(f, "OUTSIDE AGENTS ▸") {
		t.Errorf("folds at 24 rows:\n%s", f)
	}
}

// Spec: ASCII icon mode draws only ASCII.
func TestItermocASCII(t *testing.T) {
	imIcons(t, theme.IconModeASCII)
	m := itermocTestModel(t, 60, 40)
	m.ai.slotFake = map[string]aiSlot{
		"main": {cfg: ai.Config{Kind: ai.KindOllama, Model: "fake-1"}},
		"gpt":  {cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}},
	}
	m.itermocRefreshNow()
	f := strings.NewReplacer("▌", " ", "─", "-").Replace(imFrame(m)) // the cursor bar and the hairline are the shared chrome
	for i, r := range f {
		if r > 126 {
			t.Fatalf("non-ASCII rune %q (%U) at %d in\n%s", r, r, i, f)
		}
	}
}
