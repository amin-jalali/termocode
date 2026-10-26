package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
)

// AH-8: the AI write tools of the Itermoc view. Each test runs a chat
// turn (scripted model) and answers the card like the user would.

// aiWriteCard is what the user saw on the card of one call.
type aiWriteCard struct {
	opened  bool
	title   string
	body    string
	buttons []string
}

// aiWriteTurn runs one turn where the model calls tool with args; the
// card (if one opens) gets answer. It returns the tool result the model
// saw and the card.
func aiWriteTurn(t *testing.T, m *Model, tool string, args map[string]any, answer string) (string, aiWriteCard) {
	t.Helper()
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall(tool, args), {Delta: "ok"}}}
	m.ai.provider = p
	for _, sl := range []string{ai.SlotMain, ai.SlotQuick, ai.SlotLocal} { // the fast profile runs on quick
		m.ai.slotFake[sl] = aiSlot{p: p, cfg: ai.Config{Kind: ai.KindOllama, Model: "fake-1"}}
	}
	m.ai.settings.AutoApprove = nil
	var card aiWriteCard
	driveAIChat(t, m, m.sendAIChat("please"), func(c aiChatConfirmMsg) string {
		card.opened, card.body = true, c.human
		card.title = "✦ AI wants to: " + c.tool.Label()
		for _, b := range m.confirm.Buttons() {
			card.buttons = append(card.buttons, b.Title)
		}
		return answer
	})
	res := p.toolResults()
	if len(res) == 0 {
		t.Fatalf("%s: the model got no tool result", tool)
	}
	return res[0], card
}

// aiWriteModel is the profile test model with a clean session.
func aiWriteModel(t *testing.T) *Model {
	t.Helper()
	m, _ := aiItermocTestModel(t)
	return m
}

// Spec (design §5): the gates. Every tool is mutating (a read-only step
// and outside agents never get it); profile_switch is class settings,
// outside_agent_start class run, the drafts and service tools always ask.
// Remove, Sign out, Revoke, Trust and Set key have no tool.
func TestAIItermocWriteToolsGates(t *testing.T) {
	m := aiWriteModel(t)
	tools := m.aiChatTools(".", nil)
	class := map[string]string{"profile_switch": ai.ClassSettings, "outside_agent_start": ai.ClassRun}
	for _, name := range aiItermocWriteToolNames {
		tl, ok := ai.FindTool(tools, name)
		if !ok {
			t.Errorf("no tool %s", name)
			continue
		}
		if !tl.Mutating || tl.Class != class[name] || tl.Gate != nil {
			t.Errorf("%s: mutating %v class %q", name, tl.Mutating, tl.Class)
		}
		if _, ok := mcpServeRow(name); ok {
			t.Errorf("%s is offered to outside agents", name)
		}
	}
	ro := aiFilterTools(tools, agentdag.ToolRule{Preset: agentdag.PresetReadOnly})
	for _, name := range aiItermocWriteToolNames {
		if _, ok := ai.FindTool(ro, name); ok {
			t.Errorf("a read-only step gets %s", name)
		}
	}
	for _, tl := range tools {
		n := tl.Name
		for _, bad := range []string{"revoke", "sign_out", "signout", "set_key", "trust", "service_remove", "profile_delete", "slot_remove"} {
			if strings.Contains(n, bad) {
				t.Errorf("tool %s does what stays with the user", n)
			}
		}
	}
}

// Spec: every card puts Deny first (with the focus) and names the action
// on Allow; only the settings and run classes offer Always allow.
func TestAIItermocWriteCardButtons(t *testing.T) {
	m := aiWriteModel(t)
	tools := m.aiChatTools(".", nil)
	want := map[string]string{
		"profile_switch":      "Deny · Switch · Always allow settings",
		"profile_draft":       "Deny · Open in form",
		"model_slot_draft":    "Deny · Open in form",
		"service_connect":     "Deny · Open wizard",
		"service_set_access":  "Deny · Change",
		"outside_agent_start": "Deny · Start · Always allow run",
	}
	for _, name := range aiItermocWriteToolNames {
		tl, _ := ai.FindTool(tools, name)
		reply := make(chan bool, 1)
		m.onAIChatConfirm(aiChatConfirmMsg{gen: m.ai.chatGen, tool: tl, human: "x", reply: reply, ch: make(chan tea.Msg, 1)})
		var titles []string
		for _, b := range m.confirm.Buttons() {
			titles = append(titles, b.Title)
		}
		if got := strings.Join(titles, " · "); got != want[name] {
			t.Errorf("%s buttons %q, want %q", name, got, want[name])
		}
		m.answerAIToolConfirm("deny")
		if <-reply {
			t.Errorf("%s: Deny allowed", name)
		}
	}
}

// Spec (T1): "use the fast profile" → card → Allow switches; Deny keeps
// the profile and tells the model.
func TestAIProfileSwitchTool(t *testing.T) {
	m := aiWriteModel(t)
	res, card := aiWriteTurn(t, m, "profile_switch", map[string]any{"name": "fast"}, "deny")
	if !card.opened || !strings.Contains(card.body, "from default to fast") {
		t.Fatalf("card %+v", card)
	}
	if m.ai.profile == "fast" || !strings.Contains(res, "declined") {
		t.Fatalf("deny: profile %q result %q", m.ai.profile, res)
	}
	res, _ = aiWriteTurn(t, m, "profile_switch", map[string]any{"name": "fast"}, "allow")
	if m.ai.profile != "fast" || loadSession().AIProfile != "fast" || !strings.Contains(res, "Switched") {
		t.Fatalf("allow: profile %q result %q", m.ai.profile, res)
	}
	// fast's step is read-only: there the write tools are not offered at all.
	res, card = aiWriteTurn(t, m, "profile_switch", map[string]any{"name": "default"}, "allow")
	if card.opened || !strings.Contains(res, `"profile_switch" does not exist`) {
		t.Errorf("read-only step: card %v result %q", card.opened, res)
	}
	m.storeAIProfile(ai.ProfileDefault)
	res, card = aiWriteTurn(t, m, "profile_switch", map[string]any{"name": "default"}, "allow")
	if card.opened || !strings.Contains(res, "already the active profile") {
		t.Errorf("same profile: card %v result %q", card.opened, res)
	}
	res, card = aiWriteTurn(t, m, "profile_switch", map[string]any{"name": "nope"}, "allow")
	if card.opened || !strings.Contains(res, "no profile") {
		t.Errorf("unknown: card %v result %q", card.opened, res)
	}
}

// Spec (T9): a project profile the user has not trusted is not switched
// by Allow: the trust card asks the user (Deny-first rules of that card).
func TestAIProfileSwitchToolUntrustedAsksTrust(t *testing.T) {
	m := aiWriteModel(t)
	res, card := aiWriteTurn(t, m, "profile_switch", map[string]any{"name": "house"}, "allow")
	if !strings.Contains(card.body, "trust it first") {
		t.Errorf("card %q", card.body)
	}
	if m.ai.profile == "house" || !strings.Contains(res, "needs the user's trust") {
		t.Fatalf("profile %q result %q", m.ai.profile, res)
	}
	if !m.confirmOpen || m.confirmKind != confirmKindAIProfileTrust {
		t.Error("the trust card did not open")
	}
}

var aiDraftArgs = map[string]any{
	"name": "review-fix", "title": "Review then fix", "budget_usd": 0.5,
	"steps": []any{
		map[string]any{"title": "Review", "tools": "read-only", "prompt": "Review the diff.\nBe short."},
		map[string]any{"title": "Fix", "prompt": "Fix what the review found."},
	},
}

// Spec (T3): profile_draft shows the draft on the card; Allow opens the
// profile form with it and writes nothing (only the form's Save does);
// Deny opens nothing and writes nothing.
func TestAIProfileDraftTool(t *testing.T) {
	m := aiWriteModel(t)
	path, _ := ai.ProfilesPath()
	before := readOrEmpty(path)
	res, card := aiWriteTurn(t, m, "profile_draft", aiDraftArgs, "deny")
	for _, want := range []string{"New profile review-fix", "Nothing is saved", "+ title: Review then fix", "+ budget: $0.5",
		"+ 1 · Review · main · tools read-only", "+     Review the diff.", "+ 2 · Fix · main · tools all"} {
		if !strings.Contains(card.body, want) {
			t.Errorf("card has no %q:\n%s", want, card.body)
		}
	}
	if strings.Contains(card.body, "Be short") {
		t.Error("the card shows more than the first prompt line")
	}
	if m.profileFormOpen || readOrEmpty(path) != before || !strings.Contains(res, "declined") {
		t.Fatalf("deny: form %v result %q", m.profileFormOpen, res)
	}
	res, _ = aiWriteTurn(t, m, "profile_draft", aiDraftArgs, "allow")
	if !m.profileFormOpen || !strings.Contains(res, "Nothing is saved") {
		t.Fatalf("allow: form %v result %q", m.profileFormOpen, res)
	}
	if readOrEmpty(path) != before {
		t.Fatal("profile_draft wrote profiles.json")
	}
	p := m.profileForm.Profile()
	if m.profileForm.Name() != "review-fix" || p.Title != "Review then fix" || p.BudgetUSD != 0.5 || len(p.Steps) != 2 ||
		p.Steps[0].Tools.Preset != agentdag.PresetReadOnly || p.Steps[0].System != "Review the diff.\nBe short." {
		t.Errorf("form profile %+v", p)
	}
	if m.profileForm.Dirty() {
		t.Error("the form opens dirty: Cancel would ask about changes the user did not make")
	}
	res, card = aiWriteTurn(t, m, "profile_draft", aiDraftArgs, "allow")
	if !card.opened || !strings.Contains(res, "form open") {
		t.Errorf("a second draft over the open form: %q", res)
	}
}

// Spec: a draft that changes the user's own profile shows a diff; a
// built-in or project name and an unknown slot are refused before any
// card.
func TestAIProfileDraftDiffAndRefusals(t *testing.T) {
	m := aiWriteModel(t)
	path, _ := ai.ProfilesPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	old := `{"profiles":{"review-fix":{"title":"Review then fix","steps":[{"id":"step-1","title":"Review","tools":{"preset":"all"},"system":"Look."}]}}}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	m.ai.reloadProfiles()
	_, card := aiWriteTurn(t, m, "profile_draft", aiDraftArgs, "deny")
	for _, want := range []string{"Change your profile review-fix", "  title: Review then fix", "- 1 · Review · main · tools all",
		"+ 1 · Review · main · tools read-only", "+ 2 · Fix"} {
		if !strings.Contains(card.body, want) {
			t.Errorf("diff has no %q:\n%s", want, card.body)
		}
	}
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"name": "review", "steps": []any{map[string]any{"title": "A"}}}, "built-in"},
		{map[string]any{"name": "house", "steps": []any{map[string]any{"title": "A"}}}, "project"},
		{map[string]any{"name": "x", "steps": []any{map[string]any{"title": "A", "slot": "gpt"}}}, "unknown slot"},
		{map[string]any{"name": "x"}, "steps is required"},
	} {
		res, card := aiWriteTurn(t, m, "profile_draft", c.args, "allow")
		if card.opened || !strings.Contains(res, c.want) {
			t.Errorf("%v: card %v result %q", c.args, card.opened, res)
		}
	}
	if readOrEmpty(path) != old {
		t.Error("profiles.json changed")
	}
}

// Spec (T4): model_slot_draft shows the slot and what key it needs;
// Allow opens the slot form with it and writes nothing; Deny nothing.
func TestAIModelSlotDraftTool(t *testing.T) {
	m := aiWriteModel(t)
	cfgPath, _ := ai.ConfigPath()
	before := readOrEmpty(cfgPath)
	args := map[string]any{"name": "gpt", "provider": "openai", "model": "gpt-5-mini"}
	res, card := aiWriteTurn(t, m, "model_slot_draft", args, "deny")
	for _, want := range []string{"Add model slot gpt → openai · gpt-5-mini", "Nothing is saved", "OpenAI key", "Set key"} {
		if !strings.Contains(card.body, want) {
			t.Errorf("card has no %q:\n%s", want, card.body)
		}
	}
	if m.connectOpen || !strings.Contains(res, "declined") {
		t.Fatalf("deny: card open %v result %q", m.connectOpen, res)
	}
	res, _ = aiWriteTurn(t, m, "model_slot_draft", args, "allow")
	if !m.connectOpen || m.itermoc == nil || m.itermoc.form == nil || m.itermoc.form.edit {
		t.Fatalf("allow: slot form not open; result %q", res)
	}
	if m.connect.InputValue("name") != "gpt" || m.connect.InputValue("model") != "gpt-5-mini" ||
		ai.Kinds[m.connect.Choice("provider")] != ai.KindOpenAI {
		t.Errorf("form values %q %q %d", m.connect.InputValue("name"), m.connect.InputValue("model"), m.connect.Choice("provider"))
	}
	if readOrEmpty(cfgPath) != before {
		t.Fatal("model_slot_draft wrote config.json")
	}
	m.closeItermocSlotForm()
	for _, bad := range []map[string]any{
		{"name": "main", "provider": "openai"},
		{"name": "p", "provider": "openai-compatible", "base_url": "https://llm.example.com/v1?x=1"},
		{"name": "p", "provider": "openai-compatible", "base_url": "https://u:p@llm.example.com/v1"},
		{"name": "p", "provider": "nope"},
	} {
		res, card := aiWriteTurn(t, m, "model_slot_draft", bad, "allow")
		if card.opened || !strings.HasPrefix(res, "error:") {
			t.Errorf("%v: card %v result %q", bad, card.opened, res)
		}
	}
}

// Spec: no tool takes a key. A key, token or password in any argument
// (or an argument named like one) is refused before the card, and the
// refusal never repeats it.
func TestAIItermocWriteToolsRefuseSecrets(t *testing.T) {
	m := aiWriteModel(t)
	writeUserMCP(t, `{"mcpServers":{"github":{"type":"http","url":"https://api.githubcopilot.com/mcp/readonly"}}}`)
	cases := map[string]map[string]any{
		"profile_switch":      {"name": "fast", "api_key": "x"},
		"profile_draft":       {"name": "x", "steps": []any{map[string]any{"title": "A", "prompt": "use " + aiPlantAPIKey}}},
		"model_slot_draft":    {"name": "gpt", "provider": "openai", "model": aiPlantAPIKey},
		"service_connect":     {"service": "github", "token": "ghp_0123456789abcdefABCDEF0123456789"},
		"service_set_access":  {"name": "github", "writes": true, "note": "ghp_0123456789abcdefABCDEF0123456789"},
		"outside_agent_start": {"agent": "codex", "password": "hunter2"},
	}
	for _, name := range aiItermocWriteToolNames {
		res, card := aiWriteTurn(t, m, name, cases[name], "allow")
		if card.opened || !strings.Contains(res, "looks like a key") {
			t.Errorf("%s: card %v result %q", name, card.opened, res)
		}
		for _, s := range []string{aiPlantAPIKey, "ghp_0123456789", "hunter2"} {
			if strings.Contains(res, s) {
				t.Errorf("%s: the refusal repeats the secret: %q", name, res)
			}
		}
	}
}

// Spec (T6): service_connect opens the Connect wizard after the card and
// says the AI gets no token; Deny and an unknown service change nothing.
func TestAIServiceConnectTool(t *testing.T) {
	m := aiWriteModel(t)
	res, card := aiWriteTurn(t, m, "service_connect", map[string]any{"service": "github"}, "deny")
	if !strings.Contains(card.body, "Open the Connect wizard for GitHub") || !strings.Contains(card.body, "no token") {
		t.Errorf("card %q", card.body)
	}
	if !strings.Contains(res, "declined") || m.ai.mcpWiz != nil {
		t.Fatalf("deny: %q", res)
	}
	res, card = aiWriteTurn(t, m, "service_connect", map[string]any{"service": "nope"}, "allow")
	if card.opened || !strings.Contains(res, "unknown service") {
		t.Errorf("unknown: card %v result %q", card.opened, res)
	}
	// Allow: the same start as the Connect picker's row (the gh account
	// check first, as a command; this test does not run it).
	res, _ = aiWriteTurn(t, m, "service_connect", map[string]any{"service": "github"}, "allow")
	if !strings.Contains(res, "You get no token") {
		t.Errorf("allow: %q", res)
	}
	out, cmd, err := m.aiToolServiceConnect("github")
	if err != nil || cmd == nil || !strings.Contains(out, "Connect wizard for GitHub") {
		t.Errorf("allow: %q %v cmd %v", out, err, cmd != nil)
	}
}

// Spec (T6): service_set_access switches GitHub to writes after the
// card, Deny keeps it read only, and a call that changes nothing (or a
// service without the switch) is refused before any card.
func TestAIServiceSetAccessTool(t *testing.T) {
	m := aiWriteModel(t)
	writeUserMCP(t, `{"mcpServers":{"github":{"type":"http","url":"https://api.githubcopilot.com/mcp/readonly"},
		"mine":{"type":"http","url":"https://mcp.example.com/mcp"}}}`)
	m.aiMCP().Reload()
	before := readOrEmpty(mcpUserPath())
	res, card := aiWriteTurn(t, m, "service_set_access", map[string]any{"name": "github", "writes": true}, "deny")
	if !strings.Contains(card.body, "Allow writes for github") || readOrEmpty(mcpUserPath()) != before || !strings.Contains(res, "declined") {
		t.Fatalf("deny: card %q result %q", card.body, res)
	}
	res, _ = aiWriteTurn(t, m, "service_set_access", map[string]any{"name": "github", "writes": true}, "allow")
	if b := readOrEmpty(mcpUserPath()); !strings.Contains(b, `"https://api.githubcopilot.com/mcp/"`) || !strings.Contains(res, "allows writes now") {
		t.Fatalf("allow: %q\n%s", res, b)
	}
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"name": "github", "writes": true}, "already allows writes"},
		{map[string]any{"name": "mine", "writes": false}, "no read-only switch"},
		{map[string]any{"name": "github"}, "writes is required"},
	} {
		res, card := aiWriteTurn(t, m, "service_set_access", c.args, "allow")
		if card.opened || !strings.Contains(res, c.want) {
			t.Errorf("%v: card %v result %q", c.args, card.opened, res)
		}
	}
	res, _ = aiWriteTurn(t, m, "service_set_access", map[string]any{"name": "github", "writes": false}, "allow")
	if !strings.Contains(readOrEmpty(mcpUserPath()), "mcp/readonly") || !strings.Contains(res, "read only now") {
		t.Errorf("read only again: %q", res)
	}
}

// Spec (T7): outside_agent_start starts the agent through the palette's
// start path after the card; Deny starts nothing; an agent that is not
// installed is refused before the card.
func TestAIOutsideAgentStartTool(t *testing.T) {
	m := aiWriteModel(t)
	defer func(f func(string) (string, error)) { agentLookPath = f }(agentLookPath)
	agentLookPath = func(bin string) (string, error) {
		if bin == "codex" {
			return "/usr/bin/codex", nil
		}
		return "", errors.New("not found")
	}
	defer func() { aiStartOutsideAgentHook = nil }()
	var started []string
	aiStartOutsideAgentHook = func(_ *Model, a agents.Agent) (tea.Cmd, error) {
		started = append(started, a.ID)
		return nil, nil
	}
	res, card := aiWriteTurn(t, m, "outside_agent_start", map[string]any{"agent": "codex"}, "deny")
	if !strings.Contains(card.body, "Start Codex in a new terminal tab") || !strings.Contains(card.body, "termocode's tools") {
		t.Errorf("card %q", card.body)
	}
	if len(started) != 0 || !strings.Contains(res, "declined") {
		t.Fatalf("deny started %v; result %q", started, res)
	}
	res, _ = aiWriteTurn(t, m, "outside_agent_start", map[string]any{"agent": "codex"}, "allow")
	if strings.Join(started, ",") != "codex" || !strings.Contains(res, "Codex is starting") {
		t.Fatalf("allow started %v; result %q", started, res)
	}
	res, card = aiWriteTurn(t, m, "outside_agent_start", map[string]any{"agent": "claude"}, "allow")
	if card.opened || !strings.Contains(res, "not installed") || len(started) != 1 {
		t.Errorf("not installed: card %v result %q", card.opened, res)
	}
}

// Spec: model text on a card is cleaned (no escape codes, one line).
func TestAIItermocWriteCardCleansText(t *testing.T) {
	m := aiWriteModel(t)
	args := map[string]any{"name": "clean", "title": "Bad\x1b[31m title\nsecond", "steps": []any{
		map[string]any{"title": "Step\x1b]0;evil\x07 one", "prompt": "Do\x1b[2J it."}}}
	_, card := aiWriteTurn(t, m, "profile_draft", args, "deny")
	if strings.ContainsRune(card.body, 0x1b) || strings.ContainsRune(card.body, 0x07) {
		t.Errorf("escape codes on the card: %q", card.body)
	}
	if !strings.Contains(card.body, "+ title: Bad title second") {
		t.Errorf("card %q", card.body)
	}
}
