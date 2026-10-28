package app

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/addai"
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
//	full   the active profile review, an untrusted project profile, two
//	       connected AIs (Anthropic by key, Ollama), one config.json
//	       slot without a key, a service that needs a sign-in, one agent
//	providers-open   full, with Anthropic's row open and its page shown
//	providers-empty  full, but no AI connected and no slot of the user's
//	empty  a fresh install: no AI provider, no service, nothing listening
//	service  full, with the page of the service linear (sign-in ended)
//	agent    full, with the page of the outside agent claude-code
//	agents   full, with the page of the listening row
//	overview full, with the overview page (tiles, what needs you, the flow)
//	menu     full, with the actions menu of the first profile
//	slotadd     full, with the Add slot form filled in (nothing is saved)
//	slotkey     full, with the "set the key?" card after a new slot
//	slotremove  full, with the Remove card of a slot a profile uses
//	ai-<card>   full, with the card of an AI write tool (itermocAICardDemo)
//	addai-<step> full, with the Add AI card at one step (addAIDemo)
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
	s.settings.Models = map[string]ai.ModelSlot{"gpt": {Provider: ai.KindOpenAI, Model: "gpt-5-mini"}}
	s.reloadProfiles()
	s.profile = ai.ProfileReview
	m.revealItermocView(true, "")
	st := m.itermoc
	st.snap.providers = []imProvider{
		{kind: ai.KindAnthropic, name: "Anthropic", method: ai.MethodKey, label: "API key", cost: "API · pay per use",
			models: []string{"claude-fable-5-1", "claude-haiku-5-5", "claude-opus-5-5", "claude-sonnet-5-5"},
			at:     clock.Now().Add(-2 * time.Minute).Unix()},
		{kind: ai.KindOllama, name: "Ollama", method: ai.MethodLocal, label: "on this machine", cost: "local · free",
			host: "http://localhost:11434", models: []string{"llama3.2:3b", "llama3.3:70b", "qwen3:14b"},
			at: clock.Now().Add(-3 * time.Hour).Unix()},
	}
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
	st.snap.allow = map[string][]string{"claude-code": {"file_read", "tasks_run"}}
	st.fixed = true
	if arg == "profile" {
		return m.itermocProfileDemo()
	}
	if arg == "providers-empty" {
		// No AI connected and no slot of the user's: the empty state.
		st.snap.providers, st.snap.slots = nil, nil
		st.focusSection(imSecModels)
		return nil
	}
	if arg == "providers-open" {
		// Anthropic's row open (its models, Updated, Refresh, Disconnect…)
		// and its page.
		st.collapse[imSecModels] = false
		st.open[ai.KindAnthropic] = true
		row := imRow{kind: imRowProvider, sec: imSecModels, name: itermocProvName(ai.KindAnthropic)}
		m.itermoc.cursorTo(row)
		if m.itermocNarrow() {
			return nil // the list is the page: the open row shows there
		}
		return m.itermocShowPage(row, false)
	}
	if arg == "agent" || arg == "agents" {
		// claude-code's page, or the listening row's page.
		row := imRow{kind: imRowAgent, sec: imSecAgents, name: agentKey(1)}
		if arg == "agents" {
			row = imRow{kind: imRowAgentStatus, sec: imSecAgents, name: "status"}
		}
		m.itermoc.cursorTo(row)
		return m.itermocShowPage(row, false)
	}
	if arg == "overview" {
		// The overview: linear's sign-in ended, house-review waits for trust.
		st.snap.services[1].Tools = 7
		row := imRow{kind: imRowOverview, sec: imSecOverview, name: imSecOverview}
		m.itermoc.cursorTo(row)
		return m.itermocShowPage(row, false)
	}
	if arg == "menu" {
		// The actions menu of the first profile.
		row := imRow{kind: imRowProfile, sec: imSecProfiles, name: st.snap.profiles[0].p.Name}
		m.itermoc.cursorTo(row)
		return m.openItermocActions(row)
	}
	if arg == "service" {
		// linear's page: the sign-in ended and the chat lost its tools.
		st.snap.services[1].Tools = 7
		row := imRow{kind: imRowService, sec: imSecServices, name: "linear"}
		m.itermoc.cursorTo(row)
		return m.itermocShowPage(row, false)
	}
	if card, ok := strings.CutPrefix(arg, "ai-"); ok {
		return m.itermocAICardDemo(card)
	}
	if step, ok := strings.CutPrefix(arg, "addai-"); ok {
		return m.addAIDemo(step)
	}
	return m.itermocSlotDemo(arg)
}

// addAIDemo opens the Add AI card at one step over the full demo
// (addai-pick, -method, -key, -key-busy, -key-bad, -local, -url, -done).
// The card's requests are dropped: nothing is checked, run or saved.
// OpenRouter shows as added.
func (m *Model) addAIDemo(step string) tea.Cmd {
	m.addAI = addai.New(addai.Options{
		Connected: map[string]ai.ProviderEntry{ai.KindOpenRouter: {Kind: ai.KindOpenRouter, Method: ai.MethodKey}},
		KeyNote:   secretPromptNote(""),
	})
	m.addAI.SetSize(m.w, m.h)
	m.addAIOpen = true
	send := func(msgs ...tea.Msg) {
		for _, k := range msgs {
			m.addAI, _ = m.addAI.Update(k) // the request it sends is not run
		}
	}
	enter, down := tea.KeyMsg{Type: tea.KeyEnter}, tea.KeyMsg{Type: tea.KeyDown}
	pick := func(kind string) {
		for _, e := range addai.Catalog() {
			if e.Kind == kind {
				break
			}
			send(down)
		}
		send(enter)
	}
	demoKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("demo-key-not-real-0000")}
	switch step {
	case "pick":
	case "method":
		pick(ai.KindAnthropic)
	case "key", "key-busy", "key-bad", "done":
		pick(ai.KindAnthropic)
		send(down, enter)
		if step == "key" {
			break
		}
		send(demoKey, enter)
		switch step {
		case "key-bad":
			m.addAI.Fail(m.addAI.Gen(), "That key was not accepted.")
		case "done":
			m.addAI.Finish(m.addAI.Gen(), []string{"claude-fable-5-1", "claude-haiku-5-5", "claude-opus-5-5", "claude-sonnet-5-5"})
		}
	case "local":
		pick(ai.KindOllama)
	case "url":
		pick(ai.KindCompatible)
	default:
		m.addAIOpen = false
	}
	return nil
}

// itermocAICardDemo opens the card of one AI write tool (AH-8) over the
// full demo, built from the tool's own Describe. Nothing runs: the card
// is never answered.
//
//	switch  profile_switch to fast
//	draft   profile_draft that changes the user's profile second-opinion (a diff)
//	slot    model_slot_draft for a new slot work on openai
//	connect service_connect for GitHub
//	access  service_set_access: writes on for github
//	start   outside_agent_start for Codex
func (m *Model) itermocAICardDemo(which string) tea.Cmd {
	s := m.aiS()
	s.profFake = append(s.profFake, ai.Profile{Name: "second-opinion", Title: "GPT checks", Source: ai.ProfileSourceUser,
		Steps: []ai.ProfileStep{{ID: "answer", Title: "Answer", System: "Answer the question."},
			{ID: "check", Title: "Check", Slot: "gpt", System: "Check the answer for mistakes."}}})
	s.slotKnown = map[string]bool{ai.SlotMain: true, ai.SlotQuick: true, ai.SlotLocal: true, "gpt": true}
	s.reloadProfiles()
	names := map[string]string{"switch": "profile_switch", "draft": "profile_draft", "slot": "model_slot_draft",
		"connect": "service_connect", "access": "service_set_access", "start": "outside_agent_start"}
	args := map[string]map[string]any{
		"switch": {"name": ai.ProfileFast},
		"draft": {"name": "second-opinion", "title": "GPT checks, then fixes", "steps": []any{
			map[string]any{"id": "answer", "title": "Answer", "prompt": "Answer the question."},
			map[string]any{"id": "check", "title": "Check", "slot": "gpt", "tools": "read-only", "prompt": "Check the answer for mistakes."},
			map[string]any{"id": "fix", "title": "Fix", "prompt": "Fix what the check found."}}},
		"slot":    {"name": "work", "provider": ai.KindOpenAI, "model": "gpt-4.1-mini"},
		"connect": {"service": "github"},
		"access":  {"name": "github", "writes": true},
		"start":   {"agent": "codex"},
	}
	tl, ok := ai.FindTool(m.aiItermocWriteTools(nil), names[which])
	if !ok {
		return nil
	}
	m.onAIChatConfirm(aiChatConfirmMsg{gen: s.chatGen, tool: tl, human: tl.Human(args[which]), reply: make(chan bool, 1), ch: make(chan tea.Msg)})
	return nil
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
