package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/theme"
)

// The overview page (B5): the list's top row "Overview" opens a page with
// the four group tiles, every "needs you" item (each one jumps to its
// page, and carries its fix) and the active flow.

// imOverviewModel is a test model with one need in each of three groups:
// the project profile house is not trusted, the slot gpt has no key, the
// service linear lost its sign-in.
func imOverviewModel(t *testing.T) *Model {
	t.Helper()
	imIcons(t, theme.IconModeUnicode)
	m := itermocTestModel(t, 29, 50)
	m.ai.slotFake = map[string]aiSlot{
		ai.SlotMain: {cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-4-5", APIKey: "demo"}},
		"gpt":       {cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}},
	}
	m.ai.profile = ai.ProfileReview
	m.itermocRefreshNow()
	m.itermoc.fixed = true // the injected services stay (no MCP manager here)
	imInject(m, func(sn *itermocSnap) {
		sn.services = []mcp.Status{
			{Name: "github", Type: mcp.TypeHTTP, State: mcp.StateRunning, Tools: 28, Trusted: true},
			{Name: "linear", Type: mcp.TypeHTTP, State: mcp.StateSignIn, Expired: true, Trusted: true, Tools: 12},
		}
		sn.listening, sn.mode = true, "auto"
		sn.agents = []imAgent{{id: 1, name: "claude-code", since: "09:41", calls: 14}}
	})
	return m
}

// Spec: the list starts with an "Overview" row (the cursor starts on it);
// it says how many things need the user, and the filter hides it.
func TestItermocOverviewRowOnTop(t *testing.T) {
	m := imOverviewModel(t)
	rows := m.itermoc.rows()
	if len(rows) == 0 || rows[0].kind != imRowOverview {
		t.Fatalf("first row %+v, want the overview row", rows)
	}
	if r, _ := m.itermoc.current(); r.kind != imRowOverview {
		t.Errorf("the cursor starts on %+v", r)
	}
	f := imFrame(m)
	lines := strings.Split(f, "\n")
	if !strings.Contains(lines[2], "Overview") || !strings.Contains(lines[2], "3 !") {
		t.Errorf("overview row %q, want Overview and 3 !", lines[2])
	}
	m.itermoc.filter = "rev"
	if r := m.itermoc.rows(); len(r) > 0 && r[0].kind == imRowOverview {
		t.Error("the filter keeps the overview row")
	}
}

// Spec: o (and a click) on the overview row show the overview page; a
// section header still shows it too.
func TestItermocOverviewRowOpensPage(t *testing.T) {
	m := imOverviewModel(t)
	imPress(m, "o")
	if p := m.itermocPage; p == nil || p.sec != "overview" {
		t.Fatalf("page %+v, want the overview", m.itermocPage)
	}
	if r, ok := m.itermocPageRow(); !ok || r.kind != imRowOverview {
		t.Errorf("page row %+v", r)
	}
	if got := m.itermocPageTitle(); got != aiSpark()+" AI" {
		t.Errorf("title %q", got)
	}
	// A click on the row.
	m = imOverviewModel(t)
	m.itermoc.cursorTo(imRow{kind: imRowProfile, sec: imSecProfiles, name: "review"})
	var hit *hitTarget
	for _, h := range m.itermocHits(m.explorerWidth - 1) {
		if h.ID == "im:row:overview/overview" {
			h := h
			hit = &h
		}
	}
	if hit == nil {
		t.Fatal("no hit target on the overview row")
	}
	nm, _ := m.handleItermocSidebarMouse(hit.X+2, hit.Y, tea.MouseLeft)
	if p := nm.itermocPage; p == nil || p.sec != "overview" {
		t.Errorf("click: page %+v", nm.itermocPage)
	}
}

func imOverviewPage(t *testing.T) *Model {
	t.Helper()
	m := imOverviewModel(t)
	m.itermocPage = &itermocPageState{sec: imSecOverview}
	m.focus = FocusEditor
	return m
}

// Spec: the overview shows the four tiles, every "needs you" item with its
// fix pill, the active flow and three pills; every row is the pane width.
func TestItermocOverviewPage(t *testing.T) {
	m := imOverviewPage(t)
	f := imPageText(m, 160, 46)
	for _, want := range []string{"agents & connections", "PROFILES", "MODELS", "SERVICES", "OUTSIDE AGENTS",
		"review", "1 of 2 ready", "claude-code · 14 calls",
		"NEEDS YOU", "3 things", "linear", "sign-in expired · the chat lost 12 tools", "Sign in (i)",
		"gpt", "no key", "Set key (k)", "house", "not trusted yet", "Trust… (t)",
		"ACTIVE FLOW", "Reviewer", "Fixer", "answer",
		"New profile (n)", "Connect a service (c)", "Start Claude Code (s)", "1–4"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in\n%s", want, f)
		}
	}
	if strings.Contains(f, "SUMMARY") {
		t.Error("the old summary is still there")
	}
	ids := imHitIDs(m, 160, 46)
	for _, id := range []string{"im:pg:tile:1", "im:pg:tile:2", "im:pg:tile:3", "im:pg:tile:4",
		"im:pg:jump:0", "im:pg:jump:1", "im:pg:jump:2", "im:pg:fix:0", "im:pg:fix:1", "im:pg:fix:2",
		"im:pg:new", "im:pg:connect", "im:pg:start-claude", "im:pg:step:1"} {
		if !imHasHit(ids, id) {
			t.Errorf("no hit target %s in %v", id, ids)
		}
	}
	for _, sz := range [][2]int{{160, 46}, {74, 20}, {46, 21}} {
		for i, l := range m.itermocPageLayout(sz[0], sz[1]).rows {
			if w := ansi.StringWidth(l); w != sz[0] {
				t.Errorf("%dx%d: row %d is %d wide", sz[0], sz[1], i, w)
			}
		}
	}
	// Nothing needs you: one calm line, no amber list.
	imInject(m, func(sn *itermocSnap) {
		sn.services = sn.services[:1]
		sn.slots = sn.slots[:1]
		sn.profiles = sn.profiles[:1]
	})
	f = imPageText(m, 160, 46)
	if !strings.Contains(f, "nothing right now") || strings.Contains(f, "Sign in (i)") {
		t.Errorf("nothing needs you:\n%s", f)
	}
}

// Spec: a click on a "needs you" line, or Tab to it and Enter, opens its
// page and moves the list's cursor there.
func TestItermocOverviewNeedsJump(t *testing.T) {
	m := imOverviewPage(t)
	m.itermocPageVerb("jump:0", imRow{kind: imRowOverview, sec: imSecOverview, name: imSecOverview})
	if p := m.itermocPage; p.sec != imSecServices || p.name != "linear" {
		t.Errorf("click: page %+v, want linear", p)
	}
	if r, _ := m.itermoc.current(); r.kind != imRowService || r.name != "linear" {
		t.Errorf("click: list cursor on %+v", r)
	}

	m = imOverviewPage(t)
	m.itermocPageKey(tea.KeyMsg{Type: tea.KeyTab})
	m.itermocPageKey(tea.KeyMsg{Type: tea.KeyEnter})
	if p := m.itermocPage; p.sec != imSecModels || p.name != "gpt" {
		t.Errorf("Tab Enter: page %+v, want the gpt slot", p)
	}
	if m.focus != FocusEditor {
		t.Errorf("focus %v, want the page", m.focus)
	}
}

// Spec: 1–4 (and a click on a tile) go into that group: the active
// profile, the first model or service that needs you, the first agent.
func TestItermocOverviewTiles(t *testing.T) {
	cases := []struct{ key, sec, name string }{
		{"1", imSecProfiles, "default"},
		{"2", imSecModels, "gpt"},
		{"3", imSecServices, "linear"},
		{"4", imSecAgents, agentKey(1)},
	}
	for _, c := range cases {
		m := imOverviewPage(t)
		if c.key == "1" {
			for _, p := range m.itermoc.snap.profiles {
				if p.active {
					c.name = p.p.Name
				}
			}
		}
		m.itermocPageKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(c.key)})
		if p := m.itermocPage; p.sec != c.sec || p.name != c.name {
			t.Errorf("%s: page %+v, want %s/%s", c.key, p, c.sec, c.name)
		}
		m = imOverviewPage(t)
		m.itermocPageVerb("tile:"+c.key, imRow{kind: imRowOverview, sec: imSecOverview, name: imSecOverview})
		if p := m.itermocPage; p.sec != c.sec || p.name != c.name {
			t.Errorf("tile %s: page %+v, want %s/%s", c.key, p, c.sec, c.name)
		}
	}
}

// Spec: the pill of a "needs you" line runs its fix (here: the trust card
// of the project profile) without leaving the overview.
func TestItermocOverviewFixPill(t *testing.T) {
	m := imOverviewPage(t)
	m.itermocPageVerb("fix:2", imRow{kind: imRowOverview, sec: imSecOverview, name: imSecOverview})
	if pt := m.aiS().profTrust; pt == nil || pt.name != "house" {
		t.Errorf("trust card %+v, want house", pt)
	}
	if m.itermocPage.sec != imSecOverview {
		t.Errorf("the fix left the overview: %+v", m.itermocPage)
	}
}

// Spec (the shots): itermocdemo:overview shows the overview page with the
// demo's needs, at both sizes; itermocdemo:menu opens the first profile's
// actions menu, also in a narrow window where Enter opens the page.
func TestItermocDemoOverviewAndMenu(t *testing.T) {
	for _, w := range []int{200, 80} {
		m := itermocTestModel(t, 29, 50)
		m.w = w
		m.itermocDemo("overview")
		if p := m.itermocPage; p == nil || p.list || p.sec != imSecOverview {
			t.Fatalf("%d: page %+v, want the overview", w, m.itermocPage)
		}
		f := imPageText(m, 150, 40)
		for _, want := range []string{"NEEDS YOU", "linear", "the chat lost 7 tools", "house-review", "ACTIVE FLOW"} {
			if !strings.Contains(f, want) {
				t.Errorf("%d: no %q in\n%s", w, want, f)
			}
		}
		m = itermocTestModel(t, 29, 50)
		m.w = w
		m.itermocDemo("menu")
		if !m.pickerOpen || m.pickerKind != pickerKindItermocActions {
			t.Errorf("%d: no actions menu", w)
		}
	}
}
