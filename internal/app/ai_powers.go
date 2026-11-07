package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/powers"
)

// ── AI: Powers Catalog… (CMP-78) ─────────────────────────────────────────
//
// A power is a skill plus, for some, a service (internal/powers). The
// catalog lists them with "installed" and "needs <service>" hints. Enter
// shows a card, Cancel first: Install writes the skill into the user's
// skills folder (the AI sees only its name and description until a task
// needs it) and, when the service is not connected yet, opens Connect a
// Service at it (its own cards ask again). An installed power's card
// offers Remove (an edited skill stays) and Connect. The AI cannot open
// the catalog: only the user installs.

const (
	aiPowersPaletteID                         = "ai-powers"
	pickerKindAIPowers        pickerKindEnum  = 4875
	confirmKindAIPowerInstall confirmKindEnum = 4876
	confirmKindAIPowerRemove  confirmKindEnum = 4877
	aiPowersPrefix                            = "power:"
)

func init() {
	aiActionPolicies[aiPowersPaletteID] = aiNever(aiNoPicker)
}

// aiPowerConnected: the power's service has an mcp.json entry.
func aiPowerConnected(p powers.Power) bool {
	if p.MCP == "" {
		return true
	}
	list, _ := mcp.LoadAll(mcpUserPath(), mcp.ProjectPath(aiWorkspaceRoot()))
	for _, s := range list {
		if s.Name == p.MCP || strings.HasPrefix(s.Name, p.MCP+"-") {
			return true
		}
	}
	return false
}

func aiPowerService(p powers.Power) string {
	if pr, ok := mcp.PresetByName(p.MCP); ok {
		return pr.Title
	}
	return p.MCP
}

// openAIPowers opens the catalog picker.
func (m *Model) openAIPowers() tea.Cmd {
	dir := aiSkillsUserDir()
	var items []picker.Item
	for _, p := range powers.Catalog() {
		var hint []string
		if dir != "" && powers.Installed(dir, p) {
			hint = append(hint, "installed")
		}
		if p.MCP != "" {
			if aiPowerConnected(p) {
				hint = append(hint, aiPowerService(p)+" connected")
			} else {
				hint = append(hint, "needs "+aiPowerService(p))
			}
		}
		items = append(items, picker.Item{ID: aiPowersPrefix + p.ID, Title: p.Title, Hint: strings.Join(hint, " · ")})
	}
	m.picker = picker.NewItems(" "+aiSpark()+" Powers ", items)
	m.picker.SetNote([]string{"A power is a skill, and for some a service to connect.",
		"The AI loads a power only when a task needs it; Enter shows what it adds first."})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen, m.pickerKind = true, pickerKindAIPowers
	return nil
}

// onAIPowersPicked shows the power's card.
func (m *Model) onAIPowersPicked(id string) tea.Cmd {
	p, ok := powers.ByID(strings.TrimPrefix(id, aiPowersPrefix))
	if !ok {
		return nil
	}
	dir := aiSkillsUserDir()
	if dir == "" {
		return m.aiInfo("No config folder for user skills")
	}
	m.aiS().powerSel = p.ID
	path := filepath.Join(dir, p.SkillName(), "SKILL.md")
	service := ""
	if p.MCP != "" {
		service = "\n\nService: " + aiPowerService(p) + " (its tools are found only when a task needs them)."
		if !aiPowerConnected(p) {
			service += " Not connected yet: Connect a Service opens after the install and asks first."
		}
	}
	if powers.Installed(dir, p) {
		body := p.Description + "\n\nInstalled as the skill " + p.SkillName() + " in " + path + "." + service +
			"\n\nRemove deletes that file; a file you changed stays."
		btns := []confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "remove", Title: "Remove", Style: confirm.StyleDestructive}}
		if p.MCP != "" && !aiPowerConnected(p) {
			btns = append(btns, confirm.Button{ID: "connect", Title: "Connect " + aiPowerService(p)})
		}
		m.confirm = confirm.Risky("Power · "+p.Title, body, btns[0], btns[1:]...).WithScrollText()
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen, m.confirmKind = true, confirmKindAIPowerRemove
		return nil
	}
	body := p.Description + "\n\nInstall adds the skill " + p.SkillName() + " to " + path +
		". The AI sees only its name and description until a task needs it (about " +
		itoa(len(p.SkillName()+p.Description)/4+4) + " tokens per message)." + service +
		"\n\nThe text it adds:\n\n" + p.Body
	m.confirm = confirm.Risky("Install power · "+p.Title, body, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "install", Title: "Install", Style: confirm.StylePrimary}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindAIPowerInstall
	return nil
}

// answerAIPowerCard runs the card's answer ("cancel" / "": nothing).
func (m *Model) answerAIPowerCard(kind confirmKindEnum, id string) tea.Cmd {
	s := m.aiS()
	p, ok := powers.ByID(s.powerSel)
	s.powerSel = ""
	dir := aiSkillsUserDir()
	if !ok || dir == "" {
		return nil
	}
	switch {
	case kind == confirmKindAIPowerInstall && id == "install":
		if err := powers.Install(dir, p); err != nil && !errors.Is(err, os.ErrExist) {
			return m.aiToastErr("Could not install the power", err)
		}
		info := m.aiInfo("Power " + p.Title + " installed: the AI loads it when a task needs it")
		if p.MCP != "" && !aiPowerConnected(p) {
			return tea.Batch(info, m.mcpConnectServer(p.MCP))
		}
		return info
	case kind == confirmKindAIPowerRemove && id == "remove":
		err := powers.Remove(dir, p)
		switch {
		case errors.Is(err, powers.ErrChanged):
			return m.aiInfo("You changed the skill " + p.SkillName() + ": it stays; delete it by hand if you like")
		case err != nil && !errors.Is(err, os.ErrNotExist):
			return m.aiToastErr("Could not remove the power", err)
		}
		return m.aiInfo("Power " + p.Title + " removed")
	case kind == confirmKindAIPowerRemove && id == "connect":
		return m.mcpConnectServer(p.MCP)
	}
	return nil
}
