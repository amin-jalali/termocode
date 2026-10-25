package app

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/theme"
)

// imPage is a test model with the page of the object (sec, name) shown. The
// editor's buffer is not an nvim page in a unit test, so the draw functions
// are called directly.
func imPage(t *testing.T, sec, name string) *Model {
	t.Helper()
	imIcons(t, theme.IconModeUnicode)
	m := itermocTestModel(t, 29, 50)
	m.itermocPage = &itermocPageState{sec: sec, name: name}
	m.focus = FocusEditor
	return m
}

func imPageText(m *Model, w, h int) string {
	return ansi.Strip(strings.Join(m.itermocPageLayout(w, h).rows, "\n"))
}

func imHitIDs(m *Model, w, h int) []string {
	var ids []string
	for _, ht := range m.itermocPageLayout(w, h).hits {
		ids = append(ids, ht.id)
	}
	return ids
}

func imHasHit(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// Spec: the profile page shows the header, the pipeline as boxes with
// arrows, the one-line note, STEPS, SETTINGS, RECENT RUNS and the pill bar
// with the three primary actions and what the menu holds.
func TestItermocPageProfile(t *testing.T) {
	m := imPage(t, imSecProfiles, "review")
	imInject(m, func(sn *itermocSnap) {
		sn.services = []mcp.Status{{Name: "github", Type: mcp.TypeHTTP, State: mcp.StateRunning, Tools: 28, Trusted: true}}
	})
	f := imPageText(m, 160, 46)
	for _, want := range []string{"review", "you", "chat", "answer", "in the chat", "1 Reviewer", "2 Fixer", "read-only",
		"step 2 waits for step 1", "the chat can reach github", "STEPS", "SETTINGS", "categories", "budget", "source", "RECENT RUNS",
		"Use (u)", "Edit (e)", "Runs (r)", "more (Enter): Details", "▶"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in\n%s", want, f)
		}
	}
	ids := imHitIDs(m, 160, 46)
	for _, id := range []string{"im:pg:use", "im:pg:edit", "im:pg:runs", "im:pg:more", "im:pg:step:1", "im:pg:step:2"} {
		if !imHasHit(ids, id) {
			t.Errorf("no hit target %s in %v", id, ids)
		}
	}
	// Every row is exactly the pane width.
	for i, l := range m.itermocPageLayout(160, 46).rows {
		if w := ansi.StringWidth(l); w != 160 {
			t.Errorf("row %d is %d wide", i, w)
		}
	}
}

// Spec: the labels of the page's targets are drawn where the target says,
// at the sizes of the shots.
func TestItermocPageTargetsAreDrawn(t *testing.T) {
	m := imPage(t, imSecProfiles, "review")
	for _, sz := range [][2]int{{160, 46}, {46, 21}, {70, 30}} {
		lay := m.itermocPageLayout(sz[0], sz[1])
		view := strings.Join(lay.rows, "\n")
		for _, h := range lay.hits {
			if strings.HasPrefix(h.id, "im:pg:step:") {
				continue
			}
			if got := cellText(view, h.y, h.x, h.x+h.w); strings.TrimSpace(got) == "" {
				t.Errorf("%dx%d: %s is not drawn at (%d,%d)", sz[0], sz[1], h.id, h.x, h.y)
			}
		}
		if h := m.itermocPage.maxScroll; h < 0 {
			t.Errorf("scroll range %d", h)
		}
	}
}

// Spec: a profile of five steps does not run out of the pane: the pipeline
// wraps onto more rows and every step is still there.
func TestItermocPagePipelineWraps(t *testing.T) {
	m := imPage(t, imSecProfiles, "big")
	var steps []ai.ProfileStep
	for _, n := range []string{"Plan", "Code", "Test", "Review", "Ship"} {
		steps = append(steps, ai.ProfileStep{ID: strings.ToLower(n), Title: n})
	}
	imInject(m, func(sn *itermocSnap) {
		sn.profiles = append(sn.profiles, imProfile{p: ai.Profile{Name: "big", Title: "Five steps", Steps: steps}, trust: aiTrustUse})
	})
	wide := imPageText(m, 160, 46)
	for _, n := range []string{"1 Plan", "2 Code", "3 Test", "4 Review", "5 Ship", "answer"} {
		if !strings.Contains(wide, n) {
			t.Errorf("no %q in\n%s", n, wide)
		}
	}
	// A pipeline row is a row with the top-left corner of a box; one row of
	// boxes at most.
	corners := func(text string) (rows int) {
		for _, l := range strings.Split(text, "\n") {
			if strings.Contains(l, "╭") {
				rows++
			}
		}
		return rows
	}
	if got := corners(wide); got < 2 {
		t.Errorf("five steps in 160 cells: %d row of boxes, want the pipeline to wrap\n%s", got, wide)
	}
	two := imPage(t, imSecProfiles, "review")
	if got := corners(imPageText(two, 160, 46)); got != 1 {
		t.Errorf("two steps in 160 cells wrapped: %d rows of boxes", got)
	}
}

// Spec: in a narrow pane the pipeline is one line of chips; the steps and
// the pills are still there and nothing is wider than the pane.
func TestItermocPageNarrowChips(t *testing.T) {
	m := imPage(t, imSecProfiles, "review")
	f := imPageText(m, 46, 21)
	for _, want := range []string{"you", "1 Reviewer", "2 Fixer", "answer", "Use (u)", "Edit (e)", "Runs (r)"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in\n%s", want, f)
		}
	}
	if strings.Contains(f, "╭") {
		t.Errorf("boxes in a 46-cell pane:\n%s", f)
	}
	for i, l := range strings.Split(f, "\n") {
		if ansi.StringWidth(l) != 46 {
			t.Errorf("row %d is %d wide", i, ansi.StringWidth(l))
		}
	}
}

// Spec: a run that stopped is red with the reason; the other runs show
// duration and cost.
func TestItermocPageRecentRuns(t *testing.T) {
	m := imPage(t, imSecProfiles, "review")
	cost := func(c float64) *float64 { return &c }
	m.ai.conv.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "add a key card"},
		{Role: ai.StoredAssistant, Text: "stop", Run: &ai.StoredRun{ID: "r2", Profile: "review", State: ai.RunStatePaused, ElapsedMs: 130000},
			Steps: []ai.StoredStep{{ID: "a", Cost: cost(0.064)}}},
		{Role: ai.StoredUser, Text: "review my last commit"},
		{Role: ai.StoredAssistant, Text: "ok", Run: &ai.StoredRun{ID: "r1", Profile: "review", State: ai.RunStateDone, ElapsedMs: 36000},
			Steps: []ai.StoredStep{{ID: "a", Cost: cost(0.006)}, {ID: "b", Cost: cost(0.011), Files: 2}}},
		{Role: ai.StoredUser, Text: "other"},
		{Role: ai.StoredAssistant, Text: "x", Run: &ai.StoredRun{ID: "r3", Profile: "fast", State: ai.RunStateDone}},
	}
	f := imPageText(m, 160, 46)
	for _, want := range []string{"review my last commit", "36 s", "$0.017", "2 m 10 s", "$0.064", "stopped: budget reached", "2 files changed"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in\n%s", want, f)
		}
	}
	if strings.Contains(f, "r3") {
		t.Error("a run of another profile is listed")
	}
}

// Spec: a click on a list row shows its page in the one page tab; another
// row changes the same page, it does not open a second one. The actions
// menu stays on Enter.
func TestItermocClickShowsAndReusesPage(t *testing.T) {
	m := itermocTestModel(t, 70, 40)
	w := m.explorerWidth - 1
	click := func(id string) {
		t.Helper()
		for _, h := range m.itermocHits(w) {
			if h.ID == id {
				nm, _ := m.handleItermocSidebarMouse(h.X+2, h.Y, tea.MouseLeft)
				*m = nm
				return
			}
		}
		t.Fatalf("no row %s", id)
	}
	click("im:row:profiles/review")
	if m.itermocPage == nil || m.itermocPage.sec != imSecProfiles || m.itermocPage.name != "review" {
		t.Fatalf("page %+v", m.itermocPage)
	}
	if m.pickerOpen {
		t.Error("a click opened the actions menu")
	}
	first := m.itermocPage
	click("im:row:profiles/fast")
	if m.itermocPage != first || first.name != "fast" {
		t.Errorf("the page was not reused: %+v", m.itermocPage)
	}
	click("im:row:models/main")
	if m.itermocPage != first || first.sec != imSecModels || first.name != "main" {
		t.Errorf("a model row did not change the page: %+v", first)
	}
	if got := m.itermocPageTitle(); got != aiSpark()+" main" {
		t.Errorf("tab title %q", got)
	}
	imPress(m, "enter") // Enter is still the actions menu
	if !m.pickerOpen || m.pickerKind != pickerKindItermocActions {
		t.Error("Enter did not open the actions menu")
	}
}

// Spec: u, e, r work by key on the page, Enter opens today's actions menu,
// Esc goes back to the list, other letters do not reach the editor.
func TestItermocPageKeys(t *testing.T) {
	m := imPage(t, imSecProfiles, "review")
	key := func(s string) (tea.Cmd, bool) {
		var k tea.KeyMsg
		switch s {
		case "enter":
			k = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			k = tea.KeyMsg{Type: tea.KeyEsc}
		default:
			k = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
		}
		nm, cmd, ok := m.routeToItermocPage(k)
		*m = nm
		return cmd, ok
	}
	if _, ok := key("u"); !ok || m.ai.activeProfile().Name != "review" {
		t.Errorf("u did not use the profile: %q", m.ai.activeProfile().Name)
	}
	if _, ok := key("r"); !ok || !m.ai.agents.on {
		t.Error("r did not open AGENTS")
	}
	if _, ok := key("e"); !ok {
		t.Error("e was not handled")
	}
	path, _ := ai.ProfilesPath()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("e did not open the profile file: %v", err)
	}
	if _, ok := key("z"); !ok {
		t.Error("a plain letter fell through to the editor")
	}
	if _, ok := key("enter"); !ok || !m.pickerOpen || m.pickerKind != pickerKindItermocActions {
		t.Error("Enter did not open the actions menu")
	}
	m.pickerOpen = false
	if got := m.itermoc.menuRow; got.name != "review" {
		t.Errorf("the menu is about %+v", got)
	}
	if _, ok := key("esc"); !ok || m.focus != FocusExplorer {
		t.Errorf("Esc left the focus on %v", m.focus)
	}
	// With the list focused the page does not take the keys.
	if _, ok := key("u"); ok {
		t.Error("the page took a key while the list had the focus")
	}
}

// Spec: a click on a pill runs its verb; a click on a pipeline box edits
// that step (the profile form of #12 is not merged: the profile file opens).
func TestItermocPagePillsByClick(t *testing.T) {
	m := imPage(t, imSecProfiles, "review")
	m.w, m.h = 200, 50
	m.applyLayout()
	pane := m.editorPaneLayout()
	click := func(id string) {
		t.Helper()
		for _, h := range m.itermocPageTargets(pane) {
			if h.ID == id {
				nm, _, ok := m.routeToItermocPage(tea.MouseMsg{X: h.X + h.W/2, Y: h.Y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Type: tea.MouseLeft})
				if !ok {
					t.Fatalf("click on %s fell through", id)
				}
				*m = nm
				return
			}
		}
		t.Fatalf("no target %s", id)
	}
	click("im:pg:use")
	if m.ai.activeProfile().Name != "review" {
		t.Errorf("Use left %q", m.ai.activeProfile().Name)
	}
	click("im:pg:runs")
	if !m.ai.agents.on {
		t.Error("Runs did not open AGENTS")
	}
	click("im:pg:more")
	if !m.pickerOpen || m.pickerKind != pickerKindItermocActions {
		t.Error("more did not open the actions menu")
	}
	m.pickerOpen = false
	click("im:pg:step:2")
	path, _ := ai.ProfilesPath()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("a step box did not open the profile file: %v", err)
	}
	// The click belongs to the list's hit map only inside the pane.
	if _, _, ok := m.routeToItermocPage(tea.MouseMsg{X: 3, Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Type: tea.MouseLeft}); ok {
		t.Error("a click on the activity bar was taken by the page")
	}
}

// Spec: with a project profile that asks for trust the first pill is Trust…
// (amber); the page header says it asks before use.
func TestItermocPageUntrustedProfile(t *testing.T) {
	m := imPage(t, imSecProfiles, "house")
	f := imPageText(m, 160, 46)
	if !strings.Contains(f, "Trust… (t)") || !strings.Contains(f, "asks before it is used") {
		t.Errorf("untrusted page:\n%s", f)
	}
	nm, _, _ := m.routeToItermocPage(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	*m = nm
	if !m.confirmOpen || m.confirmKind != confirmKindAIProfileTrust {
		t.Error("t did not open the trust card")
	}
}

// Spec: with no profile selected (a name the list does not have) the page
// is the empty state with a way forward.
func TestItermocPageEmptyState(t *testing.T) {
	m := imPage(t, imSecProfiles, "gone")
	f := imPageText(m, 120, 40)
	if !strings.Contains(f, "NOTHING HERE YET") || !strings.Contains(f, "New profile (n)") {
		t.Errorf("empty page:\n%s", f)
	}
}

// Spec: the other kinds have a clean page of their own with their key facts
// and a few pills, never a row of buttons; a key is never on a page.
func TestItermocOtherPages(t *testing.T) {
	m := imPage(t, imSecModels, "gpt")
	m.ai.slotFake = map[string]aiSlot{
		ai.SlotMain: {cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-4-5", APIKey: imPlantedKey}},
		"gpt":       {cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}},
	}
	m.itermocRefreshNow()
	imInject(m, func(sn *itermocSnap) {
		sn.services = []mcp.Status{{Name: "linear", Type: mcp.TypeHTTP, State: mcp.StateSignIn, Expired: true, Trusted: true, Tools: 12}}
		sn.listening, sn.mode = true, "auto"
		sn.agents = []imAgent{{id: 1, name: "claude-code", since: "09:41", calls: 14, asked: 2}}
		sn.calls = []imCall{"09:42 claude-code · tasks_run · asked, you allowed"}
	})
	cases := []struct {
		sec, name string
		wants     []string
	}{
		{imSecModels, "gpt", []string{"gpt", "model slot", "openai", "gpt-5-mini", "no key", "Set key (k)", "Edit… (e)"}},
		{imSecModels, "main", []string{"anthropic", "claude-sonnet-4-5", "key ✓", "default, review", "Edit… (e)"}},
		{imSecServices, "linear", []string{"linear", "Sign-in needed", "12 tools", "Sign in (i)", "Log (l)"}},
		{imSecAgents, "status", []string{"Outside agents", "listening", "claude-code", "Start Claude Code (s)", "tasks_run"}},
		{imSecAgents, agentKey(1), []string{"claude-code", "14", "09:41"}},
		{"overview", "", []string{"SUMMARY", "profiles", "NEEDS YOU", "linear"}},
	}
	for _, c := range cases {
		m.itermocPage.sec, m.itermocPage.name = c.sec, c.name
		for _, sz := range [][2]int{{160, 46}, {46, 21}} {
			f := imPageText(m, sz[0], sz[1])
			if strings.Contains(f, imPlantedKey) || strings.Contains(f, "PLANTED") {
				t.Errorf("%s/%s leaks the key", c.sec, c.name)
			}
			for i, l := range strings.Split(f, "\n") {
				if ansi.StringWidth(l) != sz[0] {
					t.Errorf("%s/%s %dx%d: row %d is %d wide", c.sec, c.name, sz[0], sz[1], i, ansi.StringWidth(l))
				}
			}
			if sz[0] < 100 {
				continue
			}
			for _, want := range c.wants {
				if !strings.Contains(f, want) {
					t.Errorf("%s/%s: no %q in\n%s", c.sec, c.name, want, f)
				}
			}
			if strings.Count(f, " (") > 14 {
				t.Errorf("%s/%s: too many buttons:\n%s", c.sec, c.name, f)
			}
		}
	}
}

// Spec: the page's tab is named after the object; the tab strip gets that
// title for the page buffer and the buffer is not a file for the session.
func TestItermocPageTabTitleAndPath(t *testing.T) {
	m := imPage(t, imSecProfiles, "review")
	path := itermocPageTmp()
	if !isItermocPagePath(path) || isItermocPagePath("/x/review.go") {
		t.Error("isItermocPagePath")
	}
	if !isPersistedZombiePath(path) {
		t.Error("the page would come back after a restart")
	}
	bufs := []nvim.BufferInfo{{ID: 1, Path: "/x/a.go"}, {ID: 2, Path: path}}
	titled := m.titledBufs(bufs)
	if titled[1].Title != aiSpark()+" review" || titled[0].Title != "" {
		t.Errorf("titles %q %q", titled[0].Title, titled[1].Title)
	}
	m.itermocPage.sec, m.itermocPage.name = "overview", ""
	if got := m.itermocPageTitle(); got != aiSpark()+" AI" {
		t.Errorf("overview title %q", got)
	}
}

// Spec: when the list's cursor moves, a shown page follows it; a page that
// is not shown stays where it is.
func TestItermocPageFollowsCursor(t *testing.T) {
	m := imPage(t, imSecProfiles, "review")
	m.focus = FocusExplorer
	imCursorTo(t, m, imRowProfile, "review")
	// Not shown (the editor's buffer is a file): the page stays where it is.
	imPress(m, "j")
	if m.itermocPage.name != "review" {
		t.Errorf("a page that is not shown moved: %+v", m.itermocPage)
	}
	// Shown: the arrows change it at once, and a header shows the overview.
	m.editor.SetMeta(itermocPageTmp(), false, "")
	imPress(m, "j")
	if m.itermocPage.name != "local-only" {
		t.Errorf("j did not move the page: %+v", m.itermocPage)
	}
	imPress(m, "k")
	imPress(m, "k")
	if m.itermocPage.name != "review" {
		t.Errorf("k did not move the page back: %+v", m.itermocPage)
	}
	m.itermoc.cursorTo(imRow{kind: imRowSection, sec: imSecModels})
	imPress(m, "j")
	if m.itermocPage.sec != imSecModels {
		t.Errorf("the models page: %+v", m.itermocPage)
	}
}

// Spec: while the page is the shown buffer the editor area is the page, its
// tab carries the object's name and its pills are in the global hit map.
func TestItermocPageIsTheEditorArea(t *testing.T) {
	m := imPage(t, imSecProfiles, "review")
	m.w, m.h = 200, 50
	m.applyLayout()
	m.bufs = []nvim.BufferInfo{{ID: 1, Path: "/x/a.go"}, {ID: 2, Path: itermocPageTmp()}}
	m.tabs.SetBuffers(m.titledBufs(m.bufs), 2)
	m.editor.SetMeta(itermocPageTmp(), false, "")
	frame := ansi.Strip(strings.Join(m.renderBaseLines(), "\n"))
	for _, want := range []string{"1 Reviewer", "Use (u)", "a.go", "review"} {
		if !strings.Contains(frame, want) {
			t.Errorf("no %q in the frame", want)
		}
	}
	var use bool
	for _, h := range m.hitTargets() {
		use = use || h.ID == "im:pg:use"
	}
	if !use {
		t.Error("the Use pill is not in the global hit map")
	}
}

// Spec: the page stays inside the pane at small heights and scrolls.
func TestItermocPageScroll(t *testing.T) {
	m := imPage(t, imSecProfiles, "review")
	imPageText(m, 100, 18)
	if m.itermocPage.maxScroll <= 0 {
		t.Fatalf("an 18-row pane has no scroll range: %d", m.itermocPage.maxScroll)
	}
	k := tea.KeyMsg{Type: tea.KeyDown}
	nm, _, ok := m.routeToItermocPage(k)
	*m = nm
	if !ok || m.itermocPage.scroll != 1 {
		t.Errorf("down did not scroll: %d", m.itermocPage.scroll)
	}
	f := imPageText(m, 100, 18)
	if !strings.Contains(f, "Use (u)") {
		t.Errorf("the pill bar scrolled away:\n%s", f)
	}
}
