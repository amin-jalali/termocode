package app

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
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

// aiSlotModel is the model slot name runs on ("" and "main" are the
// chat's own model). ok=false when the slot has no model set.
func (s *aiState) aiSlotModel(name string) (string, bool) {
	if name == "" {
		name = ai.SlotMain
	}
	if f, ok := s.slotFake[name]; ok {
		return f.cfg.Model, f.cfg.Model != ""
	}
	if s.slots != nil {
		if cfg, ok := s.slots.Config(name); ok && cfg.Model != "" {
			return cfg.Model, true
		}
	}
	if name == ai.SlotMain && s.cfg.Kind != ai.KindNone && s.cfg.Model != "" {
		return s.cfg.Model, true
	}
	return "", false
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
