package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
)

// ── Step categories in runs (OMX-16) ──────────────────────────────────
//
// A run's categories (ai.Settings.CategoryList) pick each step's model
// slot, may narrow its tools (architect: read-only), add to its prompt
// and set the temperature. The planner hears which ones there are. A
// step that is not on the deep slot may answer ESCALATE: … to run once
// more on deep.

// aiCategoryMap is the categories by name; bad ai_categories entries go
// to the errors log once.
func (s *aiState) aiCategoryMap() map[string]ai.Category {
	list, errs := s.settings.CategoryList()
	for _, err := range errs {
		if s.catErrShown == nil {
			s.catErrShown = map[string]bool{}
		}
		if msg := err.Error(); !s.catErrShown[msg] {
			s.catErrShown[msg] = true
			recordError("[ai] " + msg)
		}
	}
	out := make(map[string]ai.Category, len(list))
	for _, c := range list {
		out[c.Name] = c
	}
	return out
}

// aiProfileCategories is prof with every category in its categories
// map: its own entry, else ai_categories' slot, else the slot of the
// category it is like. A step's category must exist.
func (s *aiState) aiProfileCategories(prof ai.Profile) (ai.Profile, map[string]ai.Category, error) {
	cats := s.aiCategoryMap()
	m := make(map[string]string, len(cats))
	for k, v := range prof.Categories {
		m[k] = v
	}
	for name, c := range cats {
		if m[name] != "" {
			continue
		}
		switch {
		case c.Slot != "":
			m[name] = c.Slot
		case name != agentdag.CategoryQuick && name != agentdag.CategoryDeep:
			m[name] = prof.SlotFor(ai.ProfileStep{Category: c.Like})
		}
	}
	prof.Categories = m
	steps := prof.Steps
	if prof.Loop != nil {
		steps = append(steps[:len(steps):len(steps)], prof.Loop.Fix)
	}
	for _, st := range steps {
		if st.Category == "" {
			continue
		}
		if _, ok := cats[st.Category]; !ok {
			return prof, cats, fmt.Errorf("step %q: unknown category %q (%s)", st.Title, st.Category, aiCategoryNames(cats))
		}
	}
	return prof, cats, nil
}

// aiCategorySlots maps every category of prof (aiProfileCategories) to
// its slot.
func aiCategorySlots(prof ai.Profile, cats map[string]ai.Category) map[string]string {
	out := prof.CategorySlots()
	for name := range cats {
		if out[name] == "" {
			out[name] = prof.SlotFor(ai.ProfileStep{Category: name})
		}
	}
	return out
}

func aiCategoryNames(cats map[string]ai.Category) string {
	var names []string
	for n := range cats {
		names = append(names, n)
	}
	sort.Strings(names)
	return "one of: " + strings.Join(names, ", ")
}

// aiResolveCategorySlots adds the slots of the run's categories to
// slots when it may need them (a planner adds steps, or a step may
// escalate) and drops the categories whose slot is not set up (but
// quick and deep, which the planner needs: their error stands).
func (s *aiState) aiResolveCategorySlots(run agentdag.Run, cats map[string]string, slots map[string]aiSlot) (map[string]string, error) {
	planned := false
	for _, n := range run.Nodes {
		planned = planned || (n.Plan && n.State != agentdag.StateCompleted)
	}
	out := map[string]string{}
	for name, slot := range cats {
		if _, ok := slots[slot]; ok {
			out[name] = slot
			continue
		}
		sl, err := s.resolveSlot(slot)
		switch {
		case err == nil:
			slots[slot] = sl
			out[name] = slot
		case planned && (name == agentdag.CategoryQuick || name == agentdag.CategoryDeep):
			return nil, fmt.Errorf("%s steps of the plan: %w", name, err)
		}
	}
	return out, nil
}

// aiCategoryNode is n with its category's tool rule: the narrower of
// the two (none < read-only < all).
func aiCategoryNode(n agentdag.Node, cats map[string]ai.Category) agentdag.Node {
	c, ok := cats[n.Category]
	if !ok || c.Tools == "" {
		return n
	}
	rank := map[string]int{agentdag.PresetNone: 0, agentdag.PresetReadOnly: 1, agentdag.PresetAll: 2, "": 2}
	if rank[c.Tools] < rank[n.Tools.Preset] {
		n.Tools.Preset = c.Tools
	}
	return n
}

// aiCategoryPrompt is what n's category adds to its system prompt, the
// planner's list of categories, and the escalation hint.
func aiCategoryPrompt(n agentdag.Node, cats map[string]ai.Category, escalate bool) string {
	var b strings.Builder
	if c, ok := cats[n.Category]; ok && c.PromptAppend != "" {
		b.WriteString("\n\n" + c.PromptAppend)
	}
	if n.Plan && len(cats) > 2 {
		var names []string
		for name := range cats {
			names = append(names, name)
		}
		sort.Strings(names)
		b.WriteString("\n\nCategories you may give a node (besides quick and deep):")
		for _, name := range names {
			if name == agentdag.CategoryQuick || name == agentdag.CategoryDeep {
				continue
			}
			b.WriteString("\n- " + name)
			if d := cats[name].Description; d != "" {
				b.WriteString(": " + d)
			}
		}
	}
	if escalate {
		b.WriteString("\n\nIf this task is clearly too hard for you, answer only with one line `" + agentrun.EscalateWord +
			": <why>`; a stronger model then does this step.")
	}
	return b.String()
}

// aiCategoryTemp is n's category temperature (nil: the provider's).
func aiCategoryTemp(n agentdag.Node, cats map[string]ai.Category) *float64 {
	if c, ok := cats[n.Category]; ok {
		return c.Temperature
	}
	return nil
}

// aiTempProvider sends every request with a temperature (providers that
// do not take one leave it out).
type aiTempProvider struct {
	ai.Provider
	temp float64
}

func (p aiTempProvider) Stream(ctx context.Context, req ai.Request) <-chan ai.Chunk {
	if req.Temperature == nil {
		t := p.temp
		req.Temperature = &t
	}
	return p.Provider.Stream(ctx, req)
}

// aiWithTemp wraps p with temp when it is set.
func aiWithTemp(p ai.Provider, temp *float64) ai.Provider {
	if temp == nil || p == nil {
		return p
	}
	return aiTempProvider{Provider: p, temp: *temp}
}

// aiRunAgain marks step id of the running run to start once more.
func (s *aiState) aiRunAgain(id string) {
	if s.runAgain == nil {
		s.runAgain = map[string]bool{}
	}
	s.runAgain[id] = true
}
