package app

import (
	"fmt"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/confirm"
)

// The pipeline of a profile as text (AU-1 picker rows, AU-2 Show
// Profile): which agents, in which order, on which model. The model of a
// step is read from its slot; nothing is built or sent.

// aiPipeStep is one step of a profile with the model its slot gives it.
type aiPipeStep struct {
	name  string // the agent's title
	slot  string // the slot it runs on
	model string // "" = the slot has no model
	step  ai.ProfileStep
}

// aiSlotConfig is the config of model slot name ("" and "main" are the
// chat's own). ok=false when there is no such slot. Nothing is built.
func (s *aiState) aiSlotConfig(name string) (ai.Config, bool) {
	if name == "" {
		name = ai.SlotMain
	}
	if f, ok := s.slotFake[name]; ok {
		return f.cfg, true
	}
	if s.slots != nil {
		if cfg, ok := s.slots.Config(name); ok {
			return cfg, true
		}
	}
	if name == ai.SlotMain && s.cfg.Kind != ai.KindNone {
		return s.cfg, true
	}
	return ai.Config{}, false
}

// aiSlotModel is the model slot name runs on. ok=false when the slot has
// no model set.
func (s *aiState) aiSlotModel(name string) (string, bool) {
	cfg, ok := s.aiSlotConfig(name)
	return cfg.Model, ok && cfg.Model != ""
}

// aiProfileSteps lists p's steps in order with their models.
func (s *aiState) aiProfileSteps(p ai.Profile) []aiPipeStep {
	out := make([]aiPipeStep, 0, len(p.Steps))
	for _, st := range p.Steps {
		name := st.Title
		if name == "" {
			name = st.ID
		}
		slot := p.SlotFor(st)
		model, _ := s.aiSlotModel(slot)
		if st.CLI != "" { // a subscription CLI (CS-1)
			slot, model = ai.CLITitle(st.CLI), "subscription"
			if st.Model != "" {
				model = st.Model + " · subscription"
			}
		}
		out = append(out, aiPipeStep{name: aiCleanText(name), slot: slot, model: model, step: st})
	}
	return out
}

var aiModelDateRe = regexp.MustCompile(`-(20\d{6}|\d{4}-\d{2}-\d{2})$`)

// aiShortModel shortens a model id for a narrow row: no vendor prefix
// ("anthropic/"), no "claude-", no date suffix.
func aiShortModel(model string) string {
	if i := strings.LastIndex(model, "/"); i >= 0 {
		model = model[i+1:]
	}
	model = strings.TrimPrefix(model, "claude-")
	return aiModelDateRe.ReplaceAllString(model, "")
}

// modelText is "claude-sonnet-4-5", or "quick: not set" for a slot
// without a model. short uses the short model name.
func (st aiPipeStep) modelText(short bool) string {
	if st.model == "" {
		return st.slot + ": not set"
	}
	if short {
		return aiShortModel(st.model)
	}
	return st.model
}

// aiPipelineText writes the pipeline of p in at most budget cells,
// dropping detail step by step: full model names, short names, models
// only, then only the number of steps.
//
//	Reviewer (claude-sonnet-4-5) → Fixer (gpt-5-mini)
//	planner decides the steps (up to 12 agents, 6 waves)
//	one step (main model: claude-sonnet-4-5)
func (s *aiState) aiPipelineText(p ai.Profile, budget int) string {
	steps := s.aiProfileSteps(p)
	fits := func(t string) bool { return runewidth.StringWidth(t) <= budget }
	if p.Loop != nil {
		rounds := p.Run("hint").Loop.MaxRounds
		first := ""
		if p.Planned() {
			first = "plan → "
		}
		for _, t := range []string{
			fmt.Sprintf("%sverify, fix until done (up to %d rounds)", first, rounds),
			fmt.Sprintf("verify, fix until done · %d rounds", rounds),
			"until done",
		} {
			if fits(t) {
				return t
			}
		}
		return "until done"
	}
	if p.Planned() {
		full := fmt.Sprintf("planner decides the steps (up to %d agents, %d waves)", agentdag.MaxNodes, agentdag.MaxLayers)
		if fits(full) {
			return full
		}
		return "planner decides the steps"
	}
	if p.Name == ai.ProfileDefault && p.Source == ai.ProfileSourceBuiltin && len(steps) == 1 {
		if steps[0].model != "" {
			if t := "one step (main model: " + steps[0].model + ")"; fits(t) {
				return t
			}
		}
		return "one step (main model)"
	}
	join := func(f func(aiPipeStep) string) string {
		parts := make([]string, len(steps))
		for i, st := range steps {
			parts[i] = f(st)
		}
		return strings.Join(parts, " → ")
	}
	levels := []func(aiPipeStep) string{
		func(st aiPipeStep) string { return st.name + " (" + st.modelText(false) + ")" },
		func(st aiPipeStep) string { return st.name + " (" + st.modelText(true) + ")" },
		func(st aiPipeStep) string { return st.modelText(true) },
	}
	for _, f := range levels {
		if t := join(f); fits(t) {
			return t
		}
	}
	return fmt.Sprintf("%d step%s", len(steps), plural(len(steps)))
}

// ── AI: Show Profile (AU-2) ────────────────────────────────────────────

const confirmKindAIProfileShow confirmKindEnum = 1002

// aiProfileFrom says where p comes from.
func (m *Model) aiProfileFrom(p ai.Profile) string {
	switch p.Source {
	case ai.ProfileSourceUser:
		return "your profiles.json (AI: Edit Profiles)"
	case ai.ProfileSourceProject:
		state := "asks before it is used"
		switch m.aiProfileTrust(p) {
		case aiTrustUse:
			state = "you trust it here"
		case aiTrustRefused:
			state = "not used in this project"
		}
		return "this project's .termocode/ai-profiles.json (" + state + ")"
	}
	return "built in"
}

// aiToolsText is what a step may use, or "" when it may use everything.
func aiToolsText(r agentdag.ToolRule) string {
	var parts []string
	switch r.Preset {
	case agentdag.PresetNone:
		parts = append(parts, "none")
	case agentdag.PresetReadOnly:
		parts = append(parts, "read-only")
	}
	if len(r.Allow) > 0 {
		parts = append(parts, "only "+strings.Join(r.Allow, ", "))
	}
	if len(r.Deny) > 0 {
		parts = append(parts, "not "+strings.Join(r.Deny, ", "))
	}
	return strings.Join(parts, "; ")
}

// aiProfileShowText is the text of the Show Profile card: where the
// profile comes from and its steps, numbered, each with its agent, slot
// and model, the tools it may use (only when the profile limits them)
// and the first line of its prompt.
func (m *Model) aiProfileShowText(p ai.Profile) string {
	s := m.aiS()
	var b strings.Builder
	b.WriteString("From: " + m.aiProfileFrom(p) + "\n")
	steps := s.aiProfileSteps(p)
	switch {
	case p.Planned():
		fmt.Fprintf(&b, "The planner decides the other steps: up to %d agents in up to %d waves.\n", agentdag.MaxNodes, agentdag.MaxLayers)
	case len(steps) == 1:
		b.WriteString("One step.\n")
	case aiProfileIsGraph(p):
		b.WriteString("Steps with no dependency run side by side (waves).\n")
	default:
		fmt.Fprintf(&b, "%d steps, one after the other.\n", len(steps))
	}
	if p.BudgetUSD > 0 {
		b.WriteString("Budget: the run pauses at " + aiBudgetText(p.BudgetUSD) + ".\n")
	}
	for i, st := range steps {
		model := st.model
		if model == "" {
			model = "not set"
		}
		fmt.Fprintf(&b, "\n%d · %s · %s → %s", i+1, st.name, st.slot, model)
		if st.step.Plan {
			b.WriteString(" · plans the steps")
		}
		if len(st.step.Deps) > 0 {
			b.WriteString(" · after " + strings.Join(st.step.Deps, ", "))
		}
		if t := aiToolsText(st.step.Tools); t != "" {
			b.WriteString("\n    tools: " + t)
		}
		if head := aiProfilePromptHead(st.step.System); head != "" {
			fmt.Fprintf(&b, "\n    “%s”", head)
		}
	}
	if p.Planned() {
		cs := p.CategorySlots()
		var parts []string
		for _, c := range []string{agentdag.CategoryQuick, agentdag.CategoryDeep} {
			model, ok := s.aiSlotModel(cs[c])
			if !ok {
				model = "not set"
			}
			parts = append(parts, c+" steps: "+cs[c]+" → "+model)
		}
		b.WriteString("\n\nThe planned steps run on:\n    " + strings.Join(parts, "\n    "))
	}
	return b.String()
}

func aiProfileIsGraph(p ai.Profile) bool {
	for _, st := range p.Steps {
		if len(st.Deps) > 0 {
			return true
		}
	}
	return false
}

// showAIProfile is AI: Show Profile: a read-only card for the active
// profile.
func (m *Model) showAIProfile() tea.Cmd { return m.showAIProfileNamed("") }

// showAIProfileNamed is the details card of the profile name ("" = the
// active one); it never switches the profile.
func (m *Model) showAIProfileNamed(name string) tea.Cmd {
	s := m.aiS()
	s.reloadProfiles()
	p := s.activeProfile()
	if name != "" {
		q, ok := s.profiles.Get(name)
		if !ok {
			return m.aiInfo("No profile " + name)
		}
		p = q
	}
	title := aiSpark() + " Profile " + p.Name
	if p.Title != "" {
		title += " · " + p.Title
	}
	m.confirm = confirm.New(title, m.aiProfileShowText(p), []confirm.Button{
		{ID: "close", Title: "Close", Style: confirm.StylePrimary},
	}).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIProfileShow
	return nil
}
