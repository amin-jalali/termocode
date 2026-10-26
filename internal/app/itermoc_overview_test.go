package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
	m.itermocRefreshNow()
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
