package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/powers"
)

// Spec (CMP-78): AI: Powers Catalog… lists the built-in powers (a skill,
// and for some a service to connect) with installed / needs-service
// hints; the AI can never open it. Enter shows a card, Cancel first:
// Cancel writes nothing; Install adds the power's skill to the user's
// skills (the AI sees only its name and description until a task needs
// it). An installed power's card offers Remove (Cancel first).
func TestAIPowersCatalog(t *testing.T) {
	m, _, _, user := aiSkillsModel(t)
	if p, ok := aiActionPolicies[aiPowersPaletteID]; !ok || p.Gate != aiGateNever {
		t.Errorf("policy %+v %v", p, ok)
	}
	found := false
	for _, e := range aiPaletteItems() {
		found = found || e.id == aiPowersPaletteID && e.title == "AI: Powers Catalog…"
	}
	if !found {
		t.Error("no palette row")
	}
	for _, p := range powers.Catalog() {
		if p.MCP != "" {
			if _, ok := mcp.PresetByName(p.MCP); !ok {
				t.Errorf("%s: no Connect preset %q", p.ID, p.MCP)
			}
		}
	}
	m.dispatchAIPalette(aiPowersPaletteID)
	if !m.pickerOpen || m.pickerKind != pickerKindAIPowers {
		t.Fatal("no catalog picker")
	}
	p := powers.Catalog()[0]
	view := strings.ToLower(ansi.Strip(m.picker.View()))
	if !strings.Contains(view, strings.ToLower(p.Title)) {
		t.Errorf("picker lacks %q:\n%s", p.Title, view)
	}
	m.pickerOpen = false

	m.onAIPowersPicked(aiPowersPrefix + p.ID)
	if !m.confirmOpen || m.confirmKind != confirmKindAIPowerInstall {
		t.Fatal("no install card")
	}
	if b := m.confirm.Buttons(); len(b) < 2 || b[0].ID != "cancel" {
		t.Errorf("buttons %+v (Cancel first)", b)
	}
	m.confirmOpen = false
	m.handleAIConfirm("cancel")
	path := filepath.Join(user, p.SkillName(), "SKILL.md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Cancel wrote %s", path)
	}

	m.onAIPowersPicked(aiPowersPrefix + p.ID)
	m.confirmOpen = false
	m.handleAIConfirm("install")
	if b, err := os.ReadFile(path); err != nil || string(b) != p.SkillText() {
		t.Fatalf("skill %q %v", b, err)
	}
	if sk, ok := aiLoadSkills(t.TempDir()).Find(p.SkillName()); !ok || sk.Source != ai.SkillSourceUser {
		t.Errorf("the power's skill is not offered: %+v %v", sk, ok)
	}

	m.onAIPowersPicked(aiPowersPrefix + p.ID)
	if !m.confirmOpen || m.confirmKind != confirmKindAIPowerRemove {
		t.Fatal("no remove card for an installed power")
	}
	if b := m.confirm.Buttons(); len(b) < 2 || b[0].ID != "cancel" {
		t.Errorf("buttons %+v (Cancel first)", b)
	}
	m.confirmOpen = false
	m.handleAIConfirm("remove")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Remove left the skill")
	}
}
