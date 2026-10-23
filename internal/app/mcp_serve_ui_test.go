package app

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
)

// mcpServeCard is the open MCP card's text.
func mcpServeCard(m *Model) string { return m.confirm.View() }

// Spec (MS-3): the card names the client that asks and the tool, and shows
// the key arguments of the call.
func TestMCPServeCardTitleNamesClient(t *testing.T) {
	m, src, hub := mcpServeTestModel(t)
	call := mcpServeStartCall(src, "claude-code", "debug_breakpoint", map[string]any{"op": "clear"})
	mcpServePumpUntil(t, m, hub, "card", func() bool { return mcpServeCardOpen(m, "claude-code") })
	view := mcpServeCard(m)
	for _, want := range []string{"claude-code wants to remove all breakpoints", "debug_breakpoint). The name", "op=clear"} {
		if !strings.Contains(view, want) {
			t.Errorf("card has no %q:\n%s", want, view)
		}
	}
	mcpServeAnswer(m, "deny")
	mcpServeAwait(t, m, hub, call)
}

// Spec (MS-3): a client that sends no name gets the generic title.
func TestMCPServeCardTitleFallback(t *testing.T) {
	m, src, hub := mcpServeTestModel(t)
	call := mcpServeStartCall(src, "", "debug_breakpoint", map[string]any{"op": "clear"})
	mcpServePumpUntil(t, m, hub, "card", func() bool { return mcpServeCardOpen(m, "An outside agent") })
	if view := mcpServeCard(m); !strings.Contains(view, "An outside agent wants to remove all breakpoints") {
		t.Errorf("card:\n%s", view)
	}
	mcpServeAnswer(m, "deny")
	mcpServeAwait(t, m, hub, call)
}

// Spec (MS-3): a secret in the arguments (a key pattern, or a value the IDE
// knows as a secret) never reaches the card, in the text or the arguments.
func TestMCPServeCardHidesSecrets(t *testing.T) {
	known := "ms3-known-secret-value-7781"
	ai.RegisterSecret(known)
	pattern := "sk-ant-api03-" + strings.Repeat("a1B2c3", 6)
	for _, secret := range []string{known, pattern} {
		m, src, hub := mcpServeTestModel(t)
		call := mcpServeStartCall(src, "claude-code", "debug_evaluate", map[string]any{"expr": "use(\"" + secret + "\")"})
		mcpServePumpUntil(t, m, hub, "card", func() bool { return mcpServeCardOpen(m, "claude-code") })
		if view := mcpServeCard(m); strings.Contains(view, secret) || strings.Contains(view, secret[:14]) {
			t.Errorf("the card shows a secret:\n%s", view)
		}
		mcpServeAnswer(m, "deny")
		mcpServeAwait(t, m, hub, call)
	}
}

// mcpServeButtonIDs are the open card's buttons.
func mcpServeButtonIDs(m *Model) string {
	var ids []string
	for _, b := range m.confirm.Buttons() {
		ids = append(ids, b.ID)
	}
	return strings.Join(ids, ",")
}

// mcpServeCallDone runs a call that must NOT ask: it finishes without a
// card ever opening.
func mcpServeCallDone(t *testing.T, m *Model, hub *mcpServeHub, src *mcpServeSource, client, tool string, args map[string]any) mcpServeTestCall {
	t.Helper()
	ch := mcpServeStartCall(src, client, tool, args)
	got := mcpServeAwait(t, m, hub, ch)
	if m.confirmOpen {
		t.Fatalf("%s %s asked", client, tool)
	}
	return got
}

// mcpServeAskAndAnswer runs a call that must ask, then presses id.
func mcpServeAskAndAnswer(t *testing.T, m *Model, hub *mcpServeHub, src *mcpServeSource, client, tool string, args map[string]any, id string) mcpServeTestCall {
	t.Helper()
	ch := mcpServeStartCall(src, client, tool, args)
	mcpServePumpUntil(t, m, hub, "card of "+client+" "+tool, func() bool { return mcpServeCardOpen(m, client) || mcpServeCardOpen(m, "An outside agent") })
	if id == "always" && !strings.Contains(mcpServeButtonIDs(m), "always") {
		t.Fatalf("no Always allow button: %s", mcpServeButtonIDs(m))
	}
	mcpServeAnswer(m, id)
	return mcpServeAwait(t, m, hub, ch)
}

var mcpServeClear = map[string]any{"op": "clear"}

// Spec (MS-3): Always allow skips the card next time for the same client
// and tool only. Another client asks, another tool of the same client asks.
func TestMCPServeAlwaysAllowIsPerClientAndTool(t *testing.T) {
	m, src, hub := mcpServeTestModel(t)
	if got := mcpServeAskAndAnswer(t, m, hub, src, "claude-code", "debug_breakpoint", mcpServeClear, "always"); got.err != nil {
		t.Fatalf("always: %v", got.err)
	}
	if got := mcpServeCallDone(t, m, hub, src, "claude-code", "debug_breakpoint", mcpServeClear); got.err != nil {
		t.Fatalf("second call: %v", got.err)
	}
	lines := strings.Join(m.output.Lines(aiAuditChannel), "\n")
	if !strings.Contains(lines, "gate=confirm:always  by=mcp:claude-code") {
		t.Errorf("no audit line for the remembered allow:\n%s", lines)
	}
	// Another client still asks.
	mcpServeAskAndAnswer(t, m, hub, src, "codex", "debug_breakpoint", mcpServeClear, "deny")
	// Another tool of the same client still asks.
	mcpServeAskAndAnswer(t, m, hub, src, "claude-code", "debug_control", map[string]any{"op": "stop"}, "deny")
}

// Spec (MS-3): the answer is kept in config.json (the user's file, its own
// key) and a new listener honours it; ai_auto_approve is not touched.
func TestMCPServeAlwaysAllowPersists(t *testing.T) {
	m, src, hub := mcpServeTestModel(t)
	mcpServeAskAndAnswer(t, m, hub, src, "claude-code", "debug_breakpoint", mcpServeClear, "always")
	path, _ := settingsConfigPath()
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), `"mcp_server_auto_approve"`) || !strings.Contains(string(b), `"debug_breakpoint"`) {
		t.Fatalf("config.json: %v %s", err, b)
	}
	if got := ai.LoadSettings(path).AutoApprove; len(got) != 0 {
		t.Fatalf("ai_auto_approve changed: %v", got)
	}
	// A new listener (restart) reads it back.
	hub2 := newMCPServeHub()
	defer hub2.stop()
	src2 := newMCPServeSource(hub2, t.TempDir())
	if !src2.allow.has("claude-code", "debug_breakpoint") || src2.allow.has("codex", "debug_breakpoint") {
		t.Fatal("a new listener does not know the allow")
	}
}

// Spec (MS-3): no Always allow for a client without a name, for
// debug_evaluate (any code) or for a tool without a class; an
// execute_action allow is per action id.
func TestMCPServeAlwaysOffered(t *testing.T) {
	m, src, hub := mcpServeTestModel(t)
	ch := mcpServeStartCall(src, "", "debug_breakpoint", mcpServeClear)
	mcpServePumpUntil(t, m, hub, "card", func() bool { return mcpServeCardOpen(m, "An outside agent") })
	if ids := mcpServeButtonIDs(m); ids != "allow,deny" {
		t.Errorf("unnamed client buttons = %s", ids)
	}
	mcpServeAnswer(m, "deny")
	mcpServeAwait(t, m, hub, ch)
	ch = mcpServeStartCall(src, "claude-code", "debug_evaluate", map[string]any{"expr": "1+1"})
	mcpServePumpUntil(t, m, hub, "card", func() bool { return mcpServeCardOpen(m, "claude-code") })
	if ids := mcpServeButtonIDs(m); ids != "allow,deny" {
		t.Errorf("debug_evaluate buttons = %s", ids)
	}
	mcpServeAnswer(m, "deny")
	mcpServeAwait(t, m, hub, ch)

	act := ai.Tool{Name: "execute_action", Class: ai.ClassIDE}
	if k := mcpServeAlwaysKey(act, map[string]any{"id": "close-buffer"}); k != "execute_action:close-buffer" {
		t.Errorf("execute_action key = %q", k)
	}
	if k := mcpServeAlwaysKey(act, map[string]any{}); k != "" {
		t.Errorf("execute_action without id key = %q", k)
	}
	if k := mcpServeAlwaysKey(ai.Tool{Name: "x"}, nil); k != "" {
		t.Errorf("tool without class key = %q", k)
	}
}

// Spec (MS-3): the remembered allows are listed in AI: Agent Auto-Approve…
// and Enter revokes one; the next call asks again.
func TestMCPServeAlwaysRevoke(t *testing.T) {
	m, src, hub := mcpServeTestModel(t)
	mcpServeAskAndAnswer(t, m, hub, src, "claude-code", "debug_breakpoint", mcpServeClear, "always")
	m.openAIAutoApprovePicker()
	row := mcpAgentRowPrefix + "claude-code:debug_breakpoint"
	found := false
	for _, it := range mcpServeAllowItems() {
		found = found || it.ID == row
	}
	if !found {
		t.Fatal("the allow is not listed")
	}
	if cmd := m.onAIAutoApproveToggled(row); cmd == nil {
		t.Fatal("revoke did nothing")
	}
	for _, it := range mcpServeAllowItems() {
		if it.ID == row {
			t.Fatal("the allow is still listed")
		}
	}
	mcpServeAskAndAnswer(t, m, hub, src, "claude-code", "debug_breakpoint", mcpServeClear, "deny")
	path, _ := settingsConfigPath()
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "debug_breakpoint") {
		t.Errorf("config.json still has it: %s", b)
	}
}

// Spec (MS-3): the chat's auto-approve does not apply to outside agents,
// and an outside agent's Always allow does not apply to the chat.
func TestMCPServeAutoApproveDoesNotCross(t *testing.T) {
	m, src, hub := mcpServeTestModel(t)
	if err := m.saveAISettings(func(st *ai.Settings) { st.AutoApprove = []string{ai.ClassRun, ai.ClassIDE, ai.ClassFileWrite} }); err != nil {
		t.Fatal(err)
	}
	mcpServeAskAndAnswer(t, m, hub, src, "claude-code", "debug_breakpoint", mcpServeClear, "deny") // still asks
	// The other way: Always allow leaves the chat's list as it was.
	mcpServeAskAndAnswer(t, m, hub, src, "claude-code", "debug_breakpoint", mcpServeClear, "always")
	path, _ := settingsConfigPath()
	got := ai.LoadSettings(path).AutoApprove
	if strings.Join(got, ",") != strings.Join([]string{ai.ClassRun, ai.ClassIDE, ai.ClassFileWrite}, ",") {
		t.Errorf("ai_auto_approve = %v", got)
	}
	if err := m.saveAISettings(func(st *ai.Settings) { st.AutoApprove = nil }); err != nil {
		t.Fatal(err)
	}
	m.aiS().reload()
	if contains(m.aiS().settings.AutoApprove, "debug_breakpoint") || len(m.aiS().settings.AutoApprove) != 0 {
		t.Errorf("the chat got an outside agent's allow: %v", m.aiS().settings.AutoApprove)
	}
	if mcpServeAllowLoad()["claude-code"][0] != "debug_breakpoint" {
		t.Error("the chat's save dropped the outside agent's allow")
	}
}

// Spec (MS-3): the AI cannot set the list (settings_set refuses it).
func TestMCPServeAllowKeyIsNeverForTheAI(t *testing.T) {
	if p, ok := aiSettingPolicies["mcp_server_auto_approve"]; !ok || p.Gate != aiGateNever {
		t.Fatalf("policy = %+v ok=%v", p, ok)
	}
}

// Spec (MCP-1): the card title says what the agent wants to do in plain
// words, not the tool id; a tool with no description still reads as words.
func TestMCPServeCardTitleHumanAction(t *testing.T) {
	cases := []struct{ client, tool, human, want string }{
		{"claude-code", "tasks_run", "Run task: say hi", `claude-code wants to run task "say hi"`},
		{mcpServeGenericClient, "debug_breakpoint", "debug_breakpoint", "An outside agent wants to use debug breakpoint"},
		{"codex", "tasks_run", "Run task: " + strings.Repeat("x", 90), `codex wants to run task "` + strings.Repeat("x", 39) + `…"`},
	}
	for _, c := range cases {
		got := mcpServeCardTitle(c.client, c.tool, c.human)
		if !strings.Contains(got, c.want) {
			t.Errorf("title(%q, %q) = %q, want it to contain %q", c.tool, c.human, got, c.want)
		}
		if strings.Contains(got, "_") {
			t.Errorf("title shows a raw tool id: %q", got)
		}
	}
}

// Spec (MCP-1): the Always-allow note ends with one "…" and no extra dot.
func TestMCPServeCardNoDoubleDot(t *testing.T) {
	m, src, hub := mcpServeTestModel(t)
	call := mcpServeStartCall(src, "claude-code", "tasks_run", map[string]any{"label": "say hi"})
	mcpServePumpUntil(t, m, hub, "card", func() bool { return mcpServeCardOpen(m, "claude-code") })
	if view := mcpServeCard(m); strings.Contains(view, "….") {
		t.Errorf("card has a double dot:\n%s", view)
	}
	mcpServeAnswer(m, "deny")
	mcpServeAwait(t, m, hub, call)
}

// Spec (MCP-2): Deny is the safe choice, so it is not red and it is the
// button Enter picks. A card can open while the user types, and an Enter
// they meant for something else must not allow an outside agent. The
// buttons keep their order (Allow, Always allow, Deny).
func TestMCPServeCardDenyIsDefault(t *testing.T) {
	m, src, hub := mcpServeTestModel(t)
	call := mcpServeStartCall(src, "claude-code", "tasks_run", map[string]any{"label": "say hi"})
	mcpServePumpUntil(t, m, hub, "card", func() bool { return mcpServeCardOpen(m, "claude-code") })
	if got := mcpServeButtonIDs(m); got != "allow,always,deny" {
		t.Fatalf("buttons = %s", got)
	}
	for _, b := range m.confirm.Buttons() {
		if b.ID == "deny" && b.Style == confirm.StyleDestructive {
			t.Error("Deny is styled as destructive (red)")
		}
	}
	_, cmd := m.confirm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	sel, ok := cmd().(confirm.SelectMsg)
	if !ok || sel.ID != "deny" {
		t.Fatalf("Enter on the fresh card = %+v, want deny", cmd())
	}
	mcpServeAnswer(m, "deny")
	mcpServeAwait(t, m, hub, call)
}
