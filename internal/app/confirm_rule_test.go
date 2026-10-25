package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// C-1 / C-2: one rule for the cards of Databases, Containers, the MCP server
// and Import. A card that deletes, overwrites or sends something outside
// has the safe choice (Cancel, Deny, Not now) first and focused, so Enter
// alone does nothing risky, and its action is red. Red is never used on the
// safe choice.

// safeCardIDs are the buttons that mean "do nothing".
var safeCardIDs = map[string]bool{"cancel": true, "deny": true, "keep": true, "skip": true, "stop": true}

// focusedCardID is the button Enter picks on the open card.
func focusedCardID(t *testing.T, m Model) string {
	t.Helper()
	_, cmd := m.confirm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter on the card does nothing")
	}
	sel, ok := cmd().(confirm.SelectMsg)
	if !ok {
		t.Fatalf("Enter on the card gives %T", cmd())
	}
	return sel.ID
}

// checkCardRule checks the open card against the rule. wantRisky says that
// it has a risky action (a red button).
func checkCardRule(t *testing.T, name string, m Model, wantRisky bool) {
	t.Helper()
	if !m.confirmOpen {
		t.Fatalf("%s: no card is open", name)
	}
	var ids []string
	risky := false
	for _, b := range m.confirm.Buttons() {
		ids = append(ids, b.ID)
		if b.Style == confirm.StyleDestructive {
			risky = true
			if safeCardIDs[b.ID] {
				t.Errorf("%s: the safe button %q is red", name, b.ID)
			}
		}
	}
	if risky != wantRisky {
		t.Errorf("%s: has a red button = %v, want %v (%v)", name, risky, wantRisky, ids)
	}
	if got := focusedCardID(t, m); !safeCardIDs[got] {
		t.Errorf("%s: Enter picks %q, want the safe choice (buttons %v)", name, got, ids)
	}
}

// C-1 / C-2 (DB-19): the AI's card for a database write (and the other
// cards of the four features) puts Deny first, plain and focused; the write
// is the red action. The card of any other tool keeps its order but its
// Deny is not red either.
func TestAIToolCardsFollowTheRule(t *testing.T) {
	open := func(tool ai.Tool) Model {
		m := Model{w: 120, h: 40}
		m.onAIChatConfirm(aiChatConfirmMsg{tool: tool, human: "do it", reply: make(chan bool, 1), ch: make(chan tea.Msg)})
		return m
	}
	m := open(ai.Tool{Name: "db_query", Mutating: true})
	checkCardRule(t, "db_query", m, true)
	if got := m.confirm.Buttons()[0].ID; got != "deny" {
		t.Errorf("db_query: first button %q, want deny", got)
	}
	checkCardRule(t, "containers_control", open(ai.Tool{Name: "containers_control", Class: ai.ClassContainers, Mutating: true}), false)
	checkCardRule(t, "services_start", open(ai.Tool{Name: "services_start", Class: ai.ClassServices, Mutating: true}), false)
	checkCardRule(t, "mcp tool", open(ai.Tool{Name: mcp.WirePrefix + "github__create_issue", Mutating: true}), false)
	// Other tools: the order stays (Allow first), Deny is plain.
	other := open(ai.Tool{Name: "write_file", Class: ai.ClassFileWrite, Mutating: true})
	for _, b := range other.confirm.Buttons() {
		if b.ID == "deny" && b.Style == confirm.StyleDestructive {
			t.Error("write_file: Deny is red")
		}
	}
	if got := other.confirm.Buttons()[0].ID; got != "allow" {
		t.Errorf("write_file: first button %q, want allow (unchanged)", got)
	}
}

// C-1: "Start MCP server X?" and "its tools changed": Deny is first, plain.
func TestMCPTrustCardsFollowTheRule(t *testing.T) {
	m := Model{w: 120, h: 40}
	m.mcpTrustDialog(mcp.Server{Name: "gh", Command: "gh-mcp", Source: mcp.SourceProject})
	checkCardRule(t, "start server", m, false)
	m = Model{w: 120, h: 40}
	m.mcpTrustDialog(mcp.Server{Name: "gh", Command: "gh-mcp", TrustReason: mcp.ReasonToolsChanged})
	checkCardRule(t, "tools changed", m, false)
}

// C-1 / AH-1: the outside agent's card puts Deny first (Deny, Allow,
// Always allow) and focuses it without Right keys.
func TestMCPServeCardFocusesDenyWithoutRightKeys(t *testing.T) {
	m, src, hub := mcpServeTestModel(t)
	call := mcpServeStartCall(src, "claude-code", "tasks_run", map[string]any{"label": "say hi"})
	mcpServePumpUntil(t, m, hub, "card", func() bool { return mcpServeCardOpen(m, "claude-code") })
	checkCardRule(t, "mcp serve", *m, false)
	if got := mcpServeButtonIDs(m); got != "deny,allow,always" {
		t.Errorf("buttons = %s", got)
	}
	mcpServeAnswer(m, "deny")
	mcpServeAwait(t, m, hub, call)
}

// C-1: the MCP status cards (stop listening, disconnect) have Cancel first.
func TestMCPServeStatusCardsFollowTheRule(t *testing.T) {
	m := mcpServeStatusModel(t)
	m.onMCPServeStatusPicked("serve-stop")
	checkCardRule(t, "stop listening", *m, false)
}

// C-1: Containers. Push sends the image outside: red, Cancel first. The
// "Remove the containers?" Cancel is plain; running a project's command
// asks with Cancel first.
func TestContainersCardsFollowTheRule(t *testing.T) {
	st := svcHostState()
	m := svcTestModel(st, 100, 40)
	m.askImagePush("ghcr.io/acme/shop:latest")
	checkCardRule(t, "push image", m, true)
	if got := m.confirm.Buttons()[1]; got.ID != "push" || got.Style != confirm.StyleDestructive {
		t.Errorf("push button = %+v", got)
	}
	m = svcTestModel(svcHostState(), 100, 40)
	m.servicesAskDown()
	checkCardRule(t, "remove the containers", m, true)
	if got := m.confirm.Buttons()[0]; got.Style != confirm.StyleDefault {
		t.Errorf("Cancel style = %q, want the plain style", got.Style)
	}
	st = svcTestState()
	m = svcTestModel(st, 100, 40)
	if _, cmd := m.servicesStartLocal("web", false); cmd != nil || !m.confirmOpen {
		t.Fatalf("no run card: open=%v", m.confirmOpen)
	}
	checkCardRule(t, "run a project command", m, false)
	if got := m.confirm.Buttons()[0].ID; got != "cancel" {
		t.Errorf("run card: first button %q, want cancel", got)
	}
}

// C-1: Databases. The uncommitted-changes card has Cancel first; Roll back,
// which drops the changes, is the red action.
func TestDatabaseTxCardFollowsTheRule(t *testing.T) {
	s := newDBState()
	s.txs = map[string]*dbTxState{"local": {open: true, writes: 2}}
	m := dbTestModel(s)
	m.databaseTxCard("local", "Quit?", nil)
	checkCardRule(t, "uncommitted changes", *m, true)
	var ids []string
	for _, b := range m.confirm.Buttons() {
		ids = append(ids, b.ID)
	}
	if strings.Join(ids, ",") != "cancel,commit,rollback" {
		t.Errorf("buttons = %v", ids)
	}
}

// C-1: the confirm card can focus a button other than the first.
func TestConfirmWithFocus(t *testing.T) {
	m := confirm.New("T", "x", []confirm.Button{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}}).WithFocus(1)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if sel, ok := cmd().(confirm.SelectMsg); !ok || sel.ID != "b" {
		t.Errorf("Enter picked %+v", cmd())
	}
	if _, cmd = m.WithFocus(9).Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Fatal("a focus outside the buttons broke the card")
	}
}
