package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// Itermoc sidebar keys, row keys, section buttons, tooltips and the
// mouse (itermoc_actions.go).

func covCursorOn(t *testing.T, m *Model, match func(imRow) bool) imRow {
	t.Helper()
	for i, r := range m.itermoc.rows() {
		if match(r) {
			m.itermoc.cursor = i
			return r
		}
	}
	t.Fatalf("no such row in %+v", m.itermoc.rows())
	return imRow{}
}

// Moving and folding: g / G jump, h folds the section and goes to its
// header, l opens it, l on a row shows its log; space and arrows on the
// overview row do nothing.
func TestCovAIItermocSidebarMoves(t *testing.T) {
	m := imOverviewModel(t)
	m.focus = FocusExplorer
	imPress(m, "G")
	if m.itermoc.cursor != len(m.itermoc.rows())-1 {
		t.Errorf("G: cursor %d of %d", m.itermoc.cursor, len(m.itermoc.rows()))
	}
	imPress(m, "g")
	if m.itermoc.cursor != 0 {
		t.Errorf("g: cursor %d", m.itermoc.cursor)
	}
	before := len(m.itermoc.rows())
	for _, k := range []string{" ", "h", "right"} {
		imPress(m, k)
	}
	if len(m.itermoc.rows()) != before {
		t.Error("a fold key on the overview row changed the list")
	}
	covCursorOn(t, m, func(r imRow) bool { return r.kind == imRowProfile })
	imPress(m, "h")
	if r, _ := m.itermoc.current(); r.kind != imRowSection || !m.itermoc.collapse[imSecProfiles] {
		t.Errorf("h: cursor on %+v, folded %v", r, m.itermoc.collapse[imSecProfiles])
	}
	imPress(m, "l")
	if m.itermoc.collapse[imSecProfiles] {
		t.Error("l did not open the section")
	}
	covCursorOn(t, m, func(r imRow) bool { return r.kind == imRowService })
	imPress(m, "l") // the service log
	m.focus = FocusExplorer
	imPress(m, "esc")
	if m.focus != FocusEditor {
		t.Error("Esc did not leave the list")
	}
}

// Row keys: a disabled service is enabled with d, a model slot's key with
// k, an agent revoked with x (each asks or opens the right thing).
func TestCovAIItermocRowKeys(t *testing.T) {
	m := imOverviewModel(t)
	imInject(m, func(sn *itermocSnap) {
		sn.services = append(sn.services, mcp.Status{Name: "off", Type: mcp.TypeStdio, State: mcp.StateDisabled, Trusted: true})
	})
	row := covCursorOn(t, m, func(r imRow) bool { return r.kind == imRowService && r.name == "off" })
	if _, ok := m.itermocRowKey("d", row); !ok {
		t.Error("d on a disabled service was not taken")
	}
	slot := covCursorOn(t, m, func(r imRow) bool { return r.kind == imRowSlot })
	if _, ok := m.itermocRowKey("k", slot); !ok {
		t.Error("k on a slot was not taken")
	}
	m.promptOpen = false
	if _, ok := m.itermocRowKey("D", slot); !ok || !m.confirmOpen || m.confirm.Buttons()[0].ID == "remove" {
		t.Errorf("D on a slot: card %v, first button %+v", m.confirmOpen, m.confirm.Buttons())
	}
	m.confirmOpen = false
	agent := covCursorOn(t, m, func(r imRow) bool { return r.kind == imRowAgent })
	if _, ok := m.itermocRowKey("x", agent); !ok {
		t.Error("x on an agent was not taken")
	}
	m.confirmOpen = false
	prof := covCursorOn(t, m, func(r imRow) bool { return r.kind == imRowProfile })
	for _, k := range []string{"d", "D"} {
		if _, ok := m.itermocRowKey(k, prof); !ok {
			t.Errorf("%s on a profile was not taken", k)
		}
		m.confirmOpen, m.pickerOpen, m.promptOpen, m.profileFormOpen = false, false, false, false
	}
	if _, ok := m.itermocRowKey("f", imRow{kind: imRowProblem, name: "0"}); !ok {
		t.Error("f on a problem row was not taken")
	}
	if _, ok := m.itermocRowKey("q", prof); ok {
		t.Error("q on a profile was taken")
	}
	// A signed-out sign-in AI: i signs in again.
	imInject(m, func(sn *itermocSnap) {
		sn.providers = append(sn.providers, imProvider{kind: ai.KindClaudeOAuth, name: "Claude", method: ai.MethodCLI, signedOut: true})
	})
	if _, ok := m.itermocRowKey("i", imRow{kind: imRowProvider, sec: imSecModels, name: itermocProvName(ai.KindClaudeOAuth)}); !ok {
		t.Error("i on a signed-out provider was not taken")
	}
}

// Section buttons: each one opens its card, picker or file.
func TestCovAIItermocSectionButtons(t *testing.T) {
	m := imOverviewModel(t)
	for _, b := range [][2]string{{imSecProfiles, "json"}, {imSecModels, "add"}, {imSecModels, "slots"}, {imSecServices, "log"},
		{imSecAgents, "start-claude"}, {imSecAgents, "start-codex"}, {imSecAgents, "copy"}} {
		m.itermocButton(b[0], b[1])
		m.pickerOpen, m.promptOpen, m.connectOpen, m.confirmOpen = false, false, false, false
	}
	if m.itermocButton("nope", "x") != nil {
		t.Error("an unknown button did something")
	}
	if m.itermocStartAgent("no-such-agent") != nil {
		t.Error("an unknown agent started")
	}
}

// Tooltips for list rows: the overview, a provider's refresh and
// disconnect rows, a provider, a model, a service, an agent.
func TestCovAIItermocRowTooltips(t *testing.T) {
	cases := map[string]string{
		"im:row:" + imSecOverview + "/" + imSecOverview:                                   "Open the overview",
		"im:row:" + imSecModels + "/" + itermocProvName(ai.KindAnthropic) + ":refresh":    "Load the models again",
		"im:row:" + imSecModels + "/" + itermocProvName(ai.KindAnthropic) + ":disconnect": "Disconnect this AI",
		"im:row:" + imSecModels + "/" + itermocProvName(ai.KindAnthropic):                 "Show its models",
		"im:row:" + imSecModels + "/gpt":                                                  "Open the model page",
		"im:row:" + imSecServices + "/gh":                                                 "Open the service page",
		"im:row:" + imSecAgents + "/agent":                                                "Open the outside agents page",
		"im:sec:profiles":                                                                 "Fold section",
		"im:pg:use":                                                                       "Use this profile in the chat",
	}
	for id, want := range cases {
		name, _, _, ok := itermocTooltip(id)
		if !ok || !strings.HasPrefix(name, want) {
			t.Errorf("%s: %q %v", id, name, ok)
		}
	}
	if _, _, _, ok := itermocTooltip("im:zzz:x"); ok {
		t.Error("an unknown kind has a tooltip")
	}
	if _, _, _, ok := itermocTooltip("other"); ok {
		t.Error("a foreign id has a tooltip")
	}
}

// The mouse on the sidebar list: the wheel, a click on nothing, a probe,
// a section header and an add row.
func TestCovAIItermocSidebarMouse(t *testing.T) {
	m := imOverviewModel(t)
	before := m.itermoc.cursor
	nm, _ := m.handleItermocSidebarMouse(0, 0, tea.MouseWheelDown)
	*m = nm
	nm, _ = m.handleItermocSidebarMouse(0, 0, tea.MouseWheelUp)
	*m = nm
	if m.itermoc.cursor != before {
		t.Errorf("wheel down and up: cursor %d → %d", before, m.itermoc.cursor)
	}
	if nm, cmd := m.handleItermocSidebarMouse(0, 0, tea.MouseMotion); cmd != nil || nm.focus != m.focus {
		t.Error("a motion did something")
	}
	nm, _ = m.handleItermocSidebarMouse(-5, -5, tea.MouseLeft)
	*m = nm
	hits := m.itermocHits(m.explorerWidth - 1)
	var sec, add *hitTarget
	for _, h := range hits {
		h := h
		if sec == nil && strings.HasPrefix(h.ID, "im:sec:") {
			sec = &h
		}
		if add == nil && strings.HasPrefix(h.ID, "im:row:") && strings.Contains(h.ID, "/+") {
			add = &h
		}
	}
	if sec == nil {
		t.Fatal("no section target")
	}
	var probe string
	m.mouseProbe = &probe
	m.handleItermocSidebarMouse(sec.X, sec.Y, tea.MouseLeft)
	if probe != sec.ID {
		t.Errorf("probe = %q", probe)
	}
	m.mouseProbe = nil
	key := strings.TrimPrefix(sec.ID, "im:sec:")
	nm, _ = m.handleItermocSidebarMouse(sec.X, sec.Y, tea.MouseLeft)
	*m = nm
	if !m.itermoc.collapse[key] {
		t.Errorf("click did not fold %s", key)
	}
	if add != nil {
		nm, _ = m.handleItermocSidebarMouse(add.X, add.Y, tea.MouseLeft)
		*m = nm
	}
}
