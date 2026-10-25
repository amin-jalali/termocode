package app

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// confirmKindMCPRemove is the "Remove service?" card.
const confirmKindMCPRemove confirmKindEnum = 2210

// mcpSetDisabled turns a service off or on: "disabled": true in the file
// the entry lives in (other keys stay). An off service stays listed, is
// not started and gives the AI no tools; turning it off stops it now.
func (m *Model) mcpSetDisabled(name string, off bool) tea.Cmd {
	entries := mcpServerEntries(name)
	if len(entries) == 0 {
		return nil
	}
	if err := mcp.SetDisabled(entries[0].SourcePath, name, off); err != nil {
		return m.aiToastErr("MCP "+name, err)
	}
	mgr := m.aiMCP()
	mgr.Reload() // a changed entry is stopped there
	if off {
		mgr.Stop(name)
		return m.aiInfo("MCP " + name + ": off (Enable brings it back)")
	}
	return m.aiInfo("MCP " + name + ": on")
}

// openMCPRemove asks before a service goes. Keep is first and focused;
// Remove is the destructive button. The AI never reaches this card: the
// services list is a picker only the user opens (ai-mcp-servers is never).
func (m *Model) openMCPRemove(name string) tea.Cmd {
	entries := mcpServerEntries(name)
	if len(entries) == 0 {
		return nil
	}
	tools := 0
	for _, st := range m.aiMCP().Servers() {
		if st.Name == name && st.State != mcp.StateDisabled {
			tools = st.Tools
		}
	}
	msg := "The entry leaves " + entries[0].SourcePath + ". Its sign-in and the secrets only it uses are forgotten."
	if tools > 0 {
		msg += fmt.Sprintf(" The chat loses %d tools.", tools)
	}
	msg += "\nTo keep the entry and only stop it, choose Disable instead."
	m.aiS().mcpSel = name
	m.confirm = confirm.New("Remove service "+name+"?", msg, []confirm.Button{
		{ID: "keep", Title: "Keep"},
		{ID: "remove", Title: "Remove", Style: confirm.StyleDestructive},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindMCPRemove
	return nil
}

// mcpRemovedMsg ends a Remove.
type mcpRemovedMsg struct {
	name string
	err  error
}

// answerMCPRemove runs the card's button (Esc is Keep).
func (m *Model) answerMCPRemove(id string) tea.Cmd {
	name := m.aiS().mcpSel
	if id != "remove" || name == "" {
		return nil
	}
	return m.mcpRemove(name)
}

// mcpRemove stops the service, forgets its stored tokens and its own
// secrets (through the secret store), then deletes the entry. Values are
// never logged or shown.
func (m *Model) mcpRemove(name string) tea.Cmd {
	entries := mcpServerEntries(name)
	if len(entries) == 0 {
		return nil
	}
	e := entries[0]
	mgr := m.aiMCP()
	var own []string
	all, _ := mcp.LoadAll(mcpUserPath(), mcp.ProjectPath(aiWorkspaceRoot()))
	for _, n := range e.SecretNames() {
		shared := false
		for _, o := range all {
			if o.Name == name {
				continue
			}
			for _, on := range o.SecretNames() {
				shared = shared || on == n
			}
		}
		if !shared {
			own = append(own, n)
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		mgr.SignOut(ctx, name) // revokes where the service allows it, forgets the sign-in, stops the process
		err := (mcp.CredStore{Path: mcpCredPath()}).Forget(name, own)
		if err == nil {
			err = mcp.RemoveServer(e.SourcePath, name)
		}
		mgr.Reload()
		return mcpRemovedMsg{name: name, err: err}
	}
}
