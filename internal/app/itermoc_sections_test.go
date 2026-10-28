package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/theme"
)

// imInject replaces parts of the snapshot with fake state and keeps the
// view from reading the real state again for an hour.
func imInject(m *Model, mut func(*itermocSnap)) {
	st := m.itermocS()
	mut(&st.snap)
	st.snapAt = clock.Now().Add(time.Hour)
	st.touch()
}

const imPlantedKey = "sk-PLANTED-0123456789abcdef"

// Spec: AI PROVIDERS lists the user's own config.json slots after the
// AIs, with the model or what is missing; the built-in slots are not
// rows. The key text itself is never in the frame, the rows or the
// snapshot.
func TestItermocModelsShowKeyStateNeverKeyValue(t *testing.T) {
	imIcons(t, theme.IconModeUnicode)
	m := itermocTestModel(t, 70, 40)
	m.ai.slotFake = map[string]aiSlot{
		ai.SlotMain:  {cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-4-5", APIKey: imPlantedKey}},
		ai.SlotQuick: {cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-haiku-4-5", APIKey: imPlantedKey}},
		ai.SlotLocal: {cfg: ai.Config{Kind: ai.KindOllama, Model: "qwen2.5-coder:7b"}},
		"gpt":        {cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}},
		"work":       {cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-4-5", APIKey: imPlantedKey}},
	}
	m.ai.settings.Models = map[string]ai.ModelSlot{"gpt": {Provider: ai.KindOpenAI, Model: "gpt-5-mini"},
		"work": {Provider: ai.KindAnthropic, Model: "claude-sonnet-4-5"}}
	m.itermocRefreshNow()
	f := imFrame(m)
	for _, want := range []string{"AI PROVIDERS", "from config.json", "work", "sonnet-4-5", "gpt", "no key", "+ Add AI…"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in\n%s", want, f)
		}
	}
	for _, r := range m.itermoc.rows() {
		if r.kind == imRowSlot && r.name != "gpt" && r.name != "work" {
			t.Errorf("a built-in slot is a row: %+v", r)
		}
	}
	if badge, needs := m.itermocS().sectionBadge(imSecModels); badge != "1 !" || !needs {
		t.Errorf("the MODELS badge does not say that one key is missing: %q", badge)
	}
	for name, text := range map[string]string{
		"frame":    f,
		"raw":      m.renderItermocSidebar(m.explorerWidth-1, m.h),
		"snapshot": fmt.Sprintf("%+v %#v", m.itermoc.snap, m.itermoc.snap),
		"rows":     fmt.Sprintf("%+v", m.itermoc.rows()),
		"hits":     fmt.Sprintf("%+v", m.itermocHits(m.explorerWidth-1)),
	} {
		if strings.Contains(text, imPlantedKey) || strings.Contains(text, "PLANTED") {
			t.Errorf("%s leaks the key", name)
		}
	}
	// The actions menu of a slot with a missing key offers Set key first.
	imCursorTo(t, m, imRowSlot, "gpt")
	_, acts := m.itermocActionsFor(m.itermoc.rows()[m.itermoc.cursor])
	if len(acts) == 0 || acts[0].id != "setkey" {
		t.Errorf("slot verbs %+v", acts)
	}
}

// Spec: with no AI connected and no slot of the user's the section says
// so, and its last row is the way in (+ Add AI…).
func TestItermocModelsMainOff(t *testing.T) {
	imIcons(t, theme.IconModeUnicode)
	m := itermocTestModel(t, 70, 40)
	m.ai.slotFake, m.ai.cfg = nil, ai.Config{}
	m.ai.slots = ai.NewSlotPool(map[string]ai.Config{}, nil)
	m.itermocRefreshNow()
	f := imFrame(m)
	if !strings.Contains(f, "No AI connected yet") || !strings.Contains(f, "+ Add AI…") {
		t.Errorf("no empty state:\n%s", f)
	}
}

// Spec: SERVICES lists the services of mcp.json with their state, a
// disabled one says "off", and a service that needs a sign-in says
// "sign in" (the page has the button).
func TestItermocServicesRows(t *testing.T) {
	imIcons(t, theme.IconModeUnicode)
	m := itermocTestModel(t, 70, 40)
	writeUserMCP(t, `{"mcpServers":{"notes":{"command":"notes-mcp"},"quiet":{"command":"quiet-mcp","disabled":true}}}`)
	m.itermoc.refresh(*m, true)
	f := imFrame(m)
	for _, want := range []string{"SERVICES", "notes", "quiet", "off", "+ connect…"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in\n%s", want, f)
		}
	}
	if strings.Contains(f, "No services yet") {
		t.Error("empty state with services present")
	}
	imInject(m, func(sn *itermocSnap) {
		sn.services = append(sn.services, mcp.Status{Name: "linear", Type: mcp.TypeHTTP, State: mcp.StateSignIn, Trusted: true})
	})
	f = imFrame(m)
	if !strings.Contains(f, "sign in") || strings.Contains(f, "[") {
		t.Errorf("no sign-in word, or a button is left:\n%s", f)
	}
	if badge, needs := m.itermoc.sectionBadge(imSecServices); badge != "1 !" || !needs {
		t.Errorf("services badge %q", badge)
	}
}

// Spec: with no service the section has an empty state and the dim
// "+ connect…" row.
func TestItermocServicesEmptyState(t *testing.T) {
	imIcons(t, theme.IconModeUnicode)
	m := itermocTestModel(t, 70, 40)
	writeUserMCP(t, `{"mcpServers":{}}`)
	m.itermoc.refresh(*m, true)
	f := imFrame(m)
	for _, want := range []string{"No services yet", "+ connect…"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in\n%s", want, f)
		}
	}
}

// Spec: OUTSIDE AGENTS says whether termocode listens, lists the connected
// agents with their calls, and the last calls as plain rows.
func TestItermocOutsideAgents(t *testing.T) {
	imIcons(t, theme.IconModeUnicode)
	m := itermocTestModel(t, 80, 40)
	f := imFrame(m)
	for _, want := range []string{"OUTSIDE AGENTS", "listening", "+ start one…"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in\n%s", want, f)
		}
	}
	m.mcpServe = &mcpServeState{clients: []*mcpServeAgent{{id: 7, name: "claude-code", since: time.Date(2026, 1, 1, 9, 41, 0, 0, time.UTC), calls: 14, asked: 2}}}
	m.itermocRefreshNow()
	imInject(m, func(sn *itermocSnap) { sn.calls = []imCall{"09:42 claude-code · tasks_run · asked, you allowed"} })
	f = imFrame(m)
	for _, want := range []string{"listening", "claude-code", "14 calls"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in\n%s", want, f)
		}
	}
}
