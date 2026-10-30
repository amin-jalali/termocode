package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// The Itermoc page (itermoc_page.go): every pill's hover label names the
// same key as the pill; keys and the wheel scroll; a narrow window goes
// back to the list in the page.

func covItermocPages(t *testing.T) *Model {
	t.Helper()
	m := imPage(t, imSecProfiles, "review")
	m.ai.slotFake = map[string]aiSlot{
		ai.SlotMain: {cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-4-5", APIKey: imPlantedKey}},
		"gpt":       {cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}},
	}
	m.itermocRefreshNow()
	imInject(m, func(sn *itermocSnap) {
		sn.services = []mcp.Status{
			{Name: "linear", Type: mcp.TypeHTTP, State: mcp.StateSignIn, Expired: true, Trusted: true, Tools: 12},
			{Name: "github", Type: mcp.TypeHTTP, State: mcp.StateRunning, Tools: 28, Trusted: true},
			{Name: "off", Type: mcp.TypeStdio, State: mcp.StateDisabled, Trusted: true},
		}
		sn.listening, sn.mode = true, "auto"
		sn.agents = []imAgent{{id: 1, name: "claude-code", since: "09:41", calls: 14, asked: 2}}
	})
	return m
}

// Every pill the pages draw has a hover label with the pill's own key.
func TestCovAIItermocPillTipsMatchKeys(t *testing.T) {
	m := covItermocPages(t)
	pages := [][2]string{
		{imSecProfiles, "review"}, {imSecModels, "gpt"}, {imSecModels, "main"}, {imSecServices, "linear"},
		{imSecServices, "github"}, {imSecServices, "off"}, {imSecAgents, "status"}, {imSecAgents, agentKey(1)},
		{imSecOverview, ""}, {imSecProfiles, "no-such-profile"},
	}
	seen := map[string]bool{}
	for _, p := range pages {
		m.itermocPage.sec, m.itermocPage.name = p[0], p[1]
		for _, pill := range m.itermocPageContent().pills {
			seen[pill.verb] = true
			name, key, ok := itermocPageTip(pill.verb)
			if !ok || name == "" {
				t.Errorf("%s/%s: pill %q has no hover label", p[0], p[1], pill.verb)
				continue
			}
			if pill.key != "" && key != "" && key != pill.key {
				t.Errorf("%s/%s: pill %q shows key %q, its hover says %q", p[0], p[1], pill.verb, pill.key, key)
			}
			if n, k, _, ok := itermocTooltip("im:pg:" + pill.verb); !ok || n != name || k != key {
				t.Errorf("tooltip of %q = %q %q", pill.verb, n, k)
			}
		}
	}
	if len(seen) < 8 {
		t.Errorf("only %d pill verbs drawn: %v", len(seen), seen)
	}
	for _, v := range []string{"step:2", "tile:p", "jump:x", "fix:x", "rowfix:x", "row:x", "addai:x", "sec:x"} {
		if _, _, ok := itermocPageTip(v); !ok {
			t.Errorf("box %q has no hover label", v)
		}
	}
	if _, _, ok := itermocPageTip("no-such-verb"); ok {
		t.Error("an unknown verb has a hover label")
	}
}

// Keys on the page: ? shows the key card, up / pgdown scroll, Esc with no
// sidebar shows the list in the page; the wheel scrolls; a click off the
// pills is the page's.
func TestCovAIItermocPageKeysAndWheel(t *testing.T) {
	m := covItermocPages(t)
	imPageText(m, 100, 14)
	key := func(k tea.KeyMsg) bool {
		nm, _, ok := m.routeToItermocPage(k)
		*m = nm
		return ok
	}
	if !key(tea.KeyMsg{Type: tea.KeyPgDown}) || m.itermocPage.scroll == 0 {
		t.Errorf("pgdown: scroll %d (max %d)", m.itermocPage.scroll, m.itermocPage.maxScroll)
	}
	before := m.itermocPage.scroll
	if !key(tea.KeyMsg{Type: tea.KeyUp}) || m.itermocPage.scroll != before-1 {
		t.Errorf("up: scroll %d", m.itermocPage.scroll)
	}
	if !key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")}) {
		t.Error("? was not taken")
	}
	if key(tea.KeyMsg{Type: tea.KeyF5}) {
		t.Error("F5 was taken by the page")
	}
	m.showExp = false
	if !key(tea.KeyMsg{Type: tea.KeyEsc}) || !m.itermocPage.list {
		t.Error("Esc without a sidebar did not show the list in the page")
	}
	m.itermocPage.list = false
	m.showExp = true
	m.w, m.h = 200, 50
	m.applyLayout()
	pane := m.editorPaneLayout()
	wheel := func(b tea.MouseButton) {
		nm, _, ok := m.routeToItermocPage(tea.MouseMsg{X: pane.X + 1, Y: pane.Top + 1, Button: b, Action: tea.MouseActionPress})
		*m = nm
		if !ok {
			t.Errorf("wheel %v not taken", b)
		}
	}
	m.itermocPage.maxScroll, m.itermocPage.scroll = 10, 0
	wheel(tea.MouseButtonWheelDown)
	if m.itermocPage.scroll != 3 {
		t.Errorf("wheel down: %d", m.itermocPage.scroll)
	}
	wheel(tea.MouseButtonWheelUp)
	if m.itermocPage.scroll != 0 {
		t.Errorf("wheel up: %d", m.itermocPage.scroll)
	}
	if _, _, ok := m.routeToItermocPage(tea.MouseMsg{X: pane.X + 1, Y: pane.Top + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease}); ok {
		t.Error("a release was taken")
	}
	if _, _, ok := m.routeToItermocPage(tea.MouseMsg{X: 0, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}); ok {
		t.Error("a click outside the pane was taken")
	}
	var probe string
	m.mouseProbe = &probe
	_, _, ok := m.routeToItermocPage(tea.MouseMsg{X: pane.X + pane.W - 1, Y: pane.Top + pane.H - 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !ok || (probe != "im:pg" && !strings.HasPrefix(probe, "im:pg:")) {
		t.Errorf("probe off the pills = %q", probe)
	}
	if m.itermocPageMore(imRow{kind: imRowSection}) != nil {
		t.Error("Enter on a section row did something")
	}
}
