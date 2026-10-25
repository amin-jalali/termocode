package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// itermocDemo opens Itermoc with sample data for the shots (dev action
// "itermocdemo:full|empty"). The data is fixed: nothing reads the machine's
// own profiles, keys or services, and no key is in it.
//
//	full   the active profile review, an untrusted project profile, one
//	       model without a key, a service that needs a sign-in, one agent
//	empty  a fresh install: no AI provider, no service, nothing listening
func (m *Model) itermocDemo(arg string) tea.Cmd {
	s := m.aiS()
	m.itermoc = newItermocState()
	if arg == "empty" {
		s.profFake, s.slotFake, s.profile = nil, nil, ""
		s.cfg = ai.Config{}
		s.slots = ai.NewSlotPool(map[string]ai.Config{}, nil)
		s.reloadProfiles()
		m.revealItermocView(true, "")
		st := m.itermoc
		st.snap.services, st.snap.agents, st.snap.calls = nil, nil, nil
		st.snap.listening, st.snap.mode = false, "auto"
		st.snap.whyTitle, st.snap.whySentence = "Not listening yet", "It starts when you start an agent tab."
		st.fixed = true
		return nil
	}
	house := ai.Profile{Name: "house-review", Title: "House style review", Source: ai.ProfileSourceProject,
		Steps: []ai.ProfileStep{
			{ID: "style", Title: "Style check", Category: agentdag.CategoryQuick, System: "Check the change against docs/STYLE.md.",
				Tools: agentdag.ToolRule{Preset: agentdag.PresetReadOnly}},
			{ID: "fix", Title: "Fixer", System: "Fix the problems the style check listed.", Tools: agentdag.ToolRule{Preset: agentdag.PresetAll}},
		}}
	s.profFake = []ai.Profile{house}
	s.slotFake = map[string]aiSlot{
		ai.SlotMain:  {cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-4-5", APIKey: "demo"}},
		ai.SlotQuick: {cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-haiku-4-5", APIKey: "demo"}},
		ai.SlotLocal: {cfg: ai.Config{Kind: ai.KindOllama, Model: "qwen2.5-coder:7b"}},
		"gpt":        {cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}},
	}
	s.reloadProfiles()
	s.profile = ai.ProfileReview
	m.revealItermocView(true, "")
	st := m.itermoc
	st.snap.services = []mcp.Status{
		{Name: "github", Type: mcp.TypeHTTP, State: mcp.StateRunning, Tools: 28, Trusted: true, SignedIn: true},
		{Name: "linear", Type: mcp.TypeHTTP, State: mcp.StateSignIn, Expired: true, Trusted: true},
		{Name: "notes", Type: mcp.TypeStdio, State: mcp.StateStopped, Tools: 11, Trusted: true},
	}
	st.snap.mode, st.snap.listening = "auto", true
	st.snap.agents = []imAgent{{id: 1, name: "claude-code", since: "09:41", calls: 14, asked: 2}}
	st.snap.calls = []imCall{
		"09:42 claude-code · tasks_run · asked, you allowed",
		"09:42 claude-code · debug_start · asked, you denied",
	}
	st.fixed = true
	return nil
}
