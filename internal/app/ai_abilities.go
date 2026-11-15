package app

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Abilities (owner 2026-11-15): every tool group of a chat turn
// (aiDeferGroups) has one of 3 states, ai_abilities in config.json:
//
//   - on: its tools are in every request;
//   - on demand (the default): deferred behind tool_search;
//   - off: never sent; the AI asks with ability_request (the Deny-first
//     tool card, on the laptop and the web) when a task needs it.
//
// A group the user did not set is on demand, except the groups that
// need something to work on (db, containers, kube): they are off when
// termocode finds nothing for them: no saved connection or project
// file, no kubeconfig. A database attachment counts as found. The user
// changes the states with AI: Abilities… (Enter goes on → on demand →
// off).

// aiAbilityNeeds are the groups that start off when nothing is found.
var aiAbilityNeeds = []string{"db", "containers", "kube"}

// aiAbilityFound reports whether termocode has anything for group in
// root (tests replace it).
var aiAbilityFound = aiAbilityFoundReal

func aiAbilityFoundReal(group, root string) bool {
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	cfg, _ := configDir()
	home, _ := os.UserHomeDir()
	compose := false
	for _, n := range services.ComposeFileNames {
		compose = compose || exists(filepath.Join(root, n))
	}
	switch group {
	case "db":
		return compose || exists(filepath.Join(root, ".termocode", "databases.json")) ||
			cfg != "" && exists(filepath.Join(cfg, "db", "connections.json"))
	case "containers":
		return compose || exists(filepath.Join(root, "Dockerfile")) || exists(filepath.Join(root, "Containerfile")) ||
			exists(filepath.Join(root, ".devcontainer")) || home != "" && exists(filepath.Join(home, ".docker", "contexts", "meta"))
	case "kube":
		if os.Getenv("KUBECONFIG") != "" {
			return true
		}
		return home != "" && exists(filepath.Join(home, ".kube", "config")) || exists(filepath.Join(root, "Chart.yaml")) ||
			exists(filepath.Join(root, "kustomization.yaml")) || exists(filepath.Join(root, "k8s"))
	}
	return true
}

// aiAbilities is one turn's ability states: what the user set, what was
// found, and the groups turned on during the chat. Safe for concurrent
// use (the agent's goroutine reads it, ability_request writes it).
type aiAbilities struct {
	mu    sync.Mutex
	set   map[string]string
	found map[string]bool
}

func (a *aiAbilities) state(g string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if v := a.set[g]; v == ai.AbilityOn || v == ai.AbilityDemand || v == ai.AbilityOff {
		return v
	}
	if f, ok := a.found[g]; ok && !f {
		return ai.AbilityOff
	}
	return ai.AbilityDemand
}

// aiAbilityStates is the state of every group for root: the user's
// setting, else on demand or (nothing found) off; a preloaded group is
// found.
func aiAbilityStates(set map[string]string, root string, preload []string) *aiAbilities {
	a := &aiAbilities{set: map[string]string{}, found: map[string]bool{}}
	for k, v := range set {
		a.set[k] = v
	}
	for _, g := range aiAbilityNeeds {
		a.found[g] = contains(preload, g) || aiAbilityFound(g, root)
	}
	return a
}

// aiAbilityEnable turns group on (on demand) for this chat and in
// config.json: the user allowed it on the ability_request card.
func (a *aiAbilities) enable(_ context.Context, g string) error {
	a.mu.Lock()
	a.set[g] = ai.AbilityDemand
	a.mu.Unlock()
	path, err := ai.ConfigPath()
	if err != nil {
		return err
	}
	st := ai.LoadSettings(path)
	if st.Abilities == nil {
		st.Abilities = map[string]string{}
	}
	st.Abilities[g] = ai.AbilityDemand
	return ai.SaveSettings(path, st)
}

// ── AI: Abilities… ─────────────────────────────────────────────────────

const pickerKindAIAbilities pickerKindEnum = 2720

func aiAbilityWord(state string) string {
	switch state {
	case ai.AbilityOn:
		return "ON"
	case ai.AbilityOff:
		return "off"
	}
	return "on demand"
}

// aiAbilityItems are the picker's rows: every group, its state, and why
// when termocode chose it.
func (m *Model) aiAbilityItems() []picker.Item {
	s := m.aiS()
	a := aiAbilityStates(s.settings.Abilities, aiWorkspaceRoot(), nil)
	var items []picker.Item
	for _, g := range aiDeferGroups {
		st := a.state(g.Name)
		hint := aiAbilityWord(st)
		if _, set := s.settings.Abilities[g.Name]; !set && st == ai.AbilityOff {
			hint += " (nothing found)"
		}
		items = append(items, picker.Item{ID: g.Name, Title: g.Name + " — " + g.About, Hint: hint})
	}
	return items
}

func (m *Model) openAIAbilitiesPicker() tea.Cmd {
	m.picker = picker.NewItems(" "+theme.GlyphAI.String()+" AI: Abilities… ", m.aiAbilityItems())
	m.picker.SetWidth(mcpServeStatusWidth(m.w))
	m.picker.SetNote([]string{"Enter: ON (every request) → on demand (the AI loads it) → off (the AI asks first)."})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAIAbilities
	return nil
}

// onAIAbilityPicked moves group to its next state and saves it.
func (m *Model) onAIAbilityPicked(group string) tea.Cmd {
	cur := aiAbilityStates(m.aiS().settings.Abilities, aiWorkspaceRoot(), nil).state(group)
	next := map[string]string{ai.AbilityOn: ai.AbilityDemand, ai.AbilityDemand: ai.AbilityOff, ai.AbilityOff: ai.AbilityOn}[cur]
	err := m.saveAISettings(func(st *ai.Settings) {
		if st.Abilities == nil {
			st.Abilities = map[string]string{}
		}
		st.Abilities[group] = next
	})
	if err != nil {
		return m.aiToastErr("Settings error", err)
	}
	cmd := m.openAIAbilitiesPicker()
	m.picker.SetCursorID(group)
	return tea.Batch(cmd, m.aiInfo("AI abilities: "+group+" is "+aiAbilityWord(next)))
}
