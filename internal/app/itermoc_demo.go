package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// itermocDemo opens Itermoc with sample data for the shots (dev action
// "itermocdemo:full|empty"). The data is fixed: nothing reads the machine's
// own profiles, keys or services, and no key is in it.
//
//	profile  full, with the page of the profile review open in the editor
//	full   the active profile review, an untrusted project profile, one
//	       model without a key, a service that needs a sign-in, one agent
//	empty  a fresh install: no AI provider, no service, nothing listening
//	service  full, with the page of the service linear (sign-in ended)
//	slotadd     full, with the Add slot form filled in (nothing is saved)
//	slotkey     full, with the "set the key?" card after a new slot
//	slotremove  full, with the Remove card of a slot a profile uses
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
	if arg == "slotremove" {
		s.profFake = append(s.profFake, ai.Profile{Name: "second-opinion", Title: "GPT checks", Source: ai.ProfileSourceUser,
			Steps: []ai.ProfileStep{{ID: "answer", Title: "Answer"}, {ID: "check", Title: "Check", Slot: "gpt"}}})
	}
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
	st.snap.svc = map[string]imSvc{
		"github": {where: "remote · api.githubcopilot.com", access: "read only", browser: true, file: "~/.config/termocode/mcp.json",
			tools: []string{"issues_list", "issue_read", "pulls_list", "pull_read", "search_code", "repos_get"}},
		"linear": {where: "remote · mcp.linear.app", access: "read only", browser: true, file: "~/.config/termocode/mcp.json",
			tools: []string{"list_issues", "get_issue", "search_issues", "list_projects", "list_cycles", "list_teams", "list_comments"}},
		"notes": {where: "local · notes-mcp", file: "~/.config/termocode/mcp.json",
			tools: []string{"notes_search", "notes_read", "notes_list"}},
	}
	st.snap.mode, st.snap.listening = "auto", true
	st.snap.agents = []imAgent{{id: 1, name: "claude-code", since: "09:41", calls: 14, asked: 2}}
	st.snap.calls = []imCall{
		"09:42 claude-code · tasks_run · asked, you allowed",
		"09:42 claude-code · debug_start · asked, you denied",
	}
	st.fixed = true
	if arg == "profile" {
		return m.itermocProfileDemo()
	}
	if arg == "service" {
		// linear's page: the sign-in ended and the chat lost its tools.
		st.snap.services[1].Tools = 7
		row := imRow{kind: imRowService, sec: imSecServices, name: "linear"}
		m.itermoc.cursorTo(row)
		return m.itermocShowPage(row, false)
	}
	return m.itermocSlotDemo(arg)
}

// itermocProfileDemo is the full demo with three runs of review in the chat
// and the profile page of review open in the editor area.
func (m *Model) itermocProfileDemo() tea.Cmd {
	cost := func(c float64) *float64 { return &c }
	run := func(id, prompt, state string, ms int64, ago time.Duration, steps []ai.StoredStep) []ai.StoredMessage {
		return []ai.StoredMessage{
			{Role: ai.StoredUser, Text: prompt},
			{Role: ai.StoredAssistant, Text: "done", Run: &ai.StoredRun{ID: id, Profile: ai.ProfileReview, State: state, ElapsedMs: ms, StartedAtMs: clock.Now().Add(-ago).UnixMilli()}, Steps: steps},
		}
	}
	s := m.aiS()
	s.conv.Messages = nil
	s.conv.Messages = append(s.conv.Messages, run("r1", "add a key card to AGENTS", ai.RunStatePaused, 130000, 3*time.Hour,
		[]ai.StoredStep{{ID: "review", Cost: cost(0.021), Files: 0}, {ID: "fix", Cost: cost(0.043), Files: 1}})...)
	s.conv.Messages = append(s.conv.Messages, run("r2", "why does the toast flicker", ai.RunStateDone, 58000, 40*time.Minute,
		[]ai.StoredStep{{ID: "review", Cost: cost(0.012)}, {ID: "fix", Cost: cost(0.019), Files: 1}})...)
	s.conv.Messages = append(s.conv.Messages, run("r3", "review my last commit", ai.RunStateDone, 36000, 12*time.Minute,
		[]ai.StoredStep{{ID: "review", Cost: cost(0.006)}, {ID: "fix", Cost: cost(0.011), Files: 2}})...)
	row := imRow{kind: imRowProfile, sec: imSecProfiles, name: ai.ProfileReview}
	m.itermoc.cursorTo(row)
	return m.itermocShowPage(row, false)
}

// itermocSlotDemo opens a slot card over the full demo. Nothing is
// written: the Remove card's slot is only in memory.
func (m *Model) itermocSlotDemo(arg string) tea.Cmd {
	st := m.itermoc
	switch arg {
	case "slotadd":
		st.form = &imSlotForm{}
		return m.showItermocSlotForm(ai.KindOpenAI, "gpt-4.1-mini", "", "work")
	case "slotkey":
		st.form = &imSlotForm{saved: true, name: "work"}
		m.connect = connectcard.New(itermocSlotKeyCard("work", ai.ModelSlot{Provider: ai.KindOpenAI, Model: "gpt-4.1-mini"}, "added", imKeyMissing))
		m.connect.SetSize(m.w, m.h)
		m.connectOpen = true
	case "slotremove":
		m.aiS().settings.Models = map[string]ai.ModelSlot{"gpt": {Provider: ai.KindOpenAI, Model: "gpt-5-mini"}}
		return m.openItermocSlotRemove("gpt")
	}
	return nil
}
