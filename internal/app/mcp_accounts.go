package app

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
)

// ── Signed-in services (MCPX-38, AU-29) ────────────────────────────────
//
// AI: MCP: Signed-in Services… is one list of every MCP server with a
// stored sign-in (OAuth tokens in the credentials file), with the
// account and access from the server row. A row asks "Sign out of X?"
// on a card with Cancel first and focused; "Sign out of all" asks once
// for every one. Sign out revokes the tokens at the service when it can
// and forgets them (the same as Sign out in a server's actions). No
// token is ever shown.

const (
	pickerKindMCPAccounts        pickerKindEnum  = 3291
	confirmKindMCPAccountSignOut confirmKindEnum = 3292
)

// mcpAccountsAllID is the "Sign out of all" row.
const mcpAccountsAllID = "\x00all"

// mcpSignedIn are the names of the servers with a stored sign-in, in
// the servers list's order.
func mcpSignedIn(m *Model) []string {
	var out []string
	for _, st := range m.aiMCP().Servers() {
		if st.SignedIn {
			out = append(out, st.Name)
		}
	}
	return out
}

// mcpAccountItems are the picker rows: "Sign out of all" first when two
// or more, then one row per signed-in server.
func mcpAccountItems(m *Model) []picker.Item {
	var rows []picker.Item
	for _, st := range m.aiMCP().Servers() {
		if !st.SignedIn {
			continue
		}
		rows = append(rows, picker.Item{ID: st.Name, Title: mcpStatusIcon(st) + " " + st.Name, Hint: mcpStatusHint(st)})
	}
	if len(rows) >= 2 {
		all := picker.Item{ID: mcpAccountsAllID, Title: "Sign out of all…", Hint: strconv.Itoa(len(rows)) + " services"}
		rows = append([]picker.Item{all}, rows...)
	}
	return rows
}

// openMCPAccounts opens the list (an info toast when nothing is signed
// in).
func (m *Model) openMCPAccounts() tea.Cmd {
	mgr := m.aiMCP()
	mgr.Reload()
	items := mcpAccountItems(m)
	if len(items) == 0 {
		return m.aiInfo("No MCP service is signed in. AI: Connect a Service… signs in to one")
	}
	m.picker = picker.NewItems(" Signed-in services (Enter: sign out…) ", items)
	m.picker.SetWidth(mcpServeStatusWidth(m.w))
	m.picker.SetNote([]string{"Sign out revokes the sign-in at the service and forgets it here."})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindMCPAccounts
	return nil
}

// onMCPAccountPicked shows the Sign out card for a row.
func (m *Model) onMCPAccountPicked(id string) tea.Cmd {
	s := m.aiS()
	var names []string
	title, msg := "", ""
	if id == mcpAccountsAllID {
		names = mcpSignedIn(m)
		if len(names) == 0 {
			return nil
		}
		title = "Sign out of " + strconv.Itoa(len(names)) + " services?"
		msg = "Their tools stop working until you sign in again. The entries stay in mcp.json."
	} else {
		if !mcpServerStatusOf(m, id).SignedIn {
			return m.aiInfo("MCP " + id + ": not signed in")
		}
		names = []string{id}
		title = "Sign out of MCP " + id + "?"
		msg = "Its tools stop working until you sign in again. The entry stays in mcp.json."
	}
	s.mcpAccountsOut = names
	m.confirm = confirm.Risky(title, msg, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "signout", Title: "Sign out", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindMCPAccountSignOut
	return nil
}

// answerMCPAccountSignOut handles the card (Esc = cancel).
func (m *Model) answerMCPAccountSignOut(id string) tea.Cmd {
	s := m.aiS()
	names := s.mcpAccountsOut
	s.mcpAccountsOut = nil
	if id != "signout" || len(names) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(names))
	for _, name := range names {
		cmds = append(cmds, m.mcpSignOut(name)) // mcp_remote_ui.go: revoke and forget
	}
	return tea.Batch(cmds...)
}
