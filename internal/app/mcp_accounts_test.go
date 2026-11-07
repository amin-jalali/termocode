package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// MCPX-38 (AU-29): AI: MCP: Signed-in Services… lists every MCP server
// with a stored sign-in in one place (account and access in the hint).
// A row opens a Sign out card with Cancel first and focused; Cancel and
// Esc keep the sign-in. With two or more, "Sign out of all" asks once.
// Nothing signed in: an info toast, no empty picker.
func TestMCPSignedInServices(t *testing.T) {
	m := covMCPModel(t)
	if cmd := m.openMCPAccounts(); m.pickerOpen || cmd == nil {
		t.Fatalf("no sign-in: picker open %v, want only a toast", m.pickerOpen)
	}

	writeUserMCP(t, `{"mcpServers":{
		"plain":{"type":"http","url":"https://mcp.example.invalid/mcp"},
		"clickup":{"type":"http","url":"https://mcp.clickup.com/mcp","oauth":{"scopes":["read"]}},
		"linear":{"type":"http","url":"https://mcp.linear.app/mcp","oauth":{"scopes":["read","write"]}}}}`)
	cs := mcp.CredStore{Path: mcpCredPath()}
	_ = cs.Save("clickup", ai.MCPOAuth{Resource: "https://mcp.clickup.com/mcp", AccessToken: "at-1234567890"})
	_ = cs.Save("linear", ai.MCPOAuth{Resource: "https://mcp.linear.app/mcp", AccessToken: "at-0987654321"})
	_ = cs.SetAccount("linear", "Acme")
	m.aiMCP().Reload()

	m.openMCPAccounts()
	if !m.pickerOpen || m.pickerKind != pickerKindMCPAccounts {
		t.Fatalf("picker open %v kind %v", m.pickerOpen, m.pickerKind)
	}
	var ids []string
	for _, it := range mcpAccountItems(m) {
		ids = append(ids, it.ID)
		if strings.Contains(it.Title+it.Hint, "at-") {
			t.Errorf("a row shows a token: %+v", it)
		}
	}
	if got := strings.Join(ids, " "); got != mcpAccountsAllID+" clickup linear" {
		t.Fatalf("rows %q, want all, clickup, linear (plain has no sign-in)", got)
	}
	for _, it := range mcpAccountItems(m) {
		if it.ID == "linear" && !strings.Contains(it.Hint, "Acme") {
			t.Errorf("linear row lacks the account: %+v", it)
		}
	}

	// One row: the card puts Cancel first; Cancel and Esc keep it.
	m.pickerOpen = false
	m.onMCPAccountPicked("linear")
	if !m.confirmOpen || m.confirmKind != confirmKindMCPAccountSignOut || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card: open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	checkCardRule(t, "sign out", *m, true)
	if card := ansi.Strip(m.confirm.Box()); !strings.Contains(card, "linear") || !strings.Contains(card, "sign in again") {
		t.Errorf("card text:\n%s", card)
	}
	m.onAIConfirmClosed() // Esc
	if !serverStatus(m, "linear").SignedIn {
		t.Fatal("Esc signed out")
	}
	m.onMCPAccountPicked("linear")
	if cmd := m.answerMCPAccountSignOut("cancel"); cmd != nil {
		t.Error("Cancel returned work")
	}
	if !serverStatus(m, "linear").SignedIn {
		t.Fatal("Cancel signed out")
	}
	m.onMCPAccountPicked("linear")
	if cmd := m.answerMCPAccountSignOut("signout"); cmd == nil {
		t.Fatal("Sign out did nothing")
	}

	// All: one card names how many.
	m.onMCPAccountPicked(mcpAccountsAllID)
	if !m.confirmOpen || m.confirm.SafeID() != "cancel" {
		t.Fatal("no Cancel-first card for all")
	}
	if card := ansi.Strip(m.confirm.Box()); !strings.Contains(card, "2 services") {
		t.Errorf("all card:\n%s", card)
	}
	if got := m.aiS().mcpAccountsOut; strings.Join(got, " ") != "clickup linear" {
		t.Errorf("all would sign out %v", got)
	}
}

// The palette entry is never for the AI (it opens a picker) and the web
// parity file has it.
func TestMCPSignedInServicesPolicy(t *testing.T) {
	if _, ok := aiActionPolicies["ai-mcp-accounts"]; !ok {
		t.Error("ai-mcp-accounts has no AI policy")
	}
	found := false
	for _, c := range aiPaletteItems() {
		if c.id == "ai-mcp-accounts" {
			found = true
		}
	}
	if !found {
		t.Error("ai-mcp-accounts is not in the palette")
	}
}
