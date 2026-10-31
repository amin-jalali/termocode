package ai

import (
	"fmt"
	"sort"

	"github.com/amin-jalali/termocode/internal/agentdag"
)

// ── Step categories (OMX-16) ──────────────────────────────────────────
//
// A category is the kind of work of a run step: it picks the model slot
// (a profile's categories map, else ai_categories' slot, else the slot
// of the category it is like: quick or deep) and may narrow the step's
// tools, add to its prompt and set the temperature and reasoning level. quick and deep are
// the old two; the others are built in or come from ai_categories.

// CategoryConfig is one ai_categories entry.
type CategoryConfig struct {
	// Like is quick or deep (default deep): the slot when none is set,
	// and whether ESCALATE moves its steps to deep.
	Like         string   `json:"like,omitempty"`
	Slot         string   `json:"slot,omitempty"`
	Tools        string   `json:"tools,omitempty"` // none | read-only | all ("": the step's own)
	Temperature  *float64 `json:"temperature,omitempty"`
	PromptAppend string   `json:"prompt_append,omitempty"`
	Description  string   `json:"description,omitempty"` // for the planner
	// Reasoning (OMX-5 level, "" for the slot's) is the level of its steps.
	Reasoning string `json:"reasoning,omitempty"`
}

// Category is a category with its name.
type Category struct {
	Name string
	CategoryConfig
	Builtin bool
}

// CategoryPromptMax caps a category's prompt_append (runes).
const CategoryPromptMax = 4000

// BuiltinCategories are the categories there always are, in order.
func BuiltinCategories() []Category {
	b := func(name, like, tools, desc, prompt string) Category {
		return Category{Name: name, Builtin: true, CategoryConfig: CategoryConfig{Like: like, Tools: tools, Description: desc, PromptAppend: prompt}}
	}
	return []Category{
		b(agentdag.CategoryQuick, agentdag.CategoryQuick, "", "small, clear tasks (a cheaper model)", ""),
		b(agentdag.CategoryDeep, agentdag.CategoryDeep, "", "hard reasoning, review or the final check", ""),
		b("visual", agentdag.CategoryDeep, "", "user interface work: layout, styles, colours, how it looks",
			"You work on the user interface: layout, styles, colours, spacing and how it looks. Keep the project's own look, "+
				"and check the result against what the user asked."),
		b("writing", agentdag.CategoryQuick, "", "docs, comments, messages and other prose",
			"You write prose: docs, comments, commit messages. Use short, clear sentences in the project's tone; change no code "+
				"unless the task says so."),
		b("architect", agentdag.CategoryDeep, agentdag.PresetReadOnly, "design advice only: it reads, it never changes files",
			"You are the architect: read the code, then give design advice with the trade-offs and one clear recommendation. "+
				"You change no files and run no commands."),
		b("ultrabrain", agentdag.CategoryDeep, "", "the hardest problems, thought through with care",
			"This is a hard problem. Think it through step by step, check each assumption against the code, and say how sure "+
				"you are of the answer."),
	}
}

// CategoryList is the built-in categories changed by ai_categories,
// then the new ones (by name); errs name the entries left out.
func (s Settings) CategoryList() ([]Category, []error) {
	out := BuiltinCategories()
	idx := map[string]int{}
	for i, c := range out {
		idx[c.Name] = i
	}
	var names []string
	for n := range s.Categories {
		names = append(names, n)
	}
	sort.Strings(names)
	var errs []error
	for _, n := range names {
		c := s.Categories[n]
		if err := checkCategory(n, c); err != nil {
			errs = append(errs, err)
			continue
		}
		c.Reasoning = NormalizeReasoning(c.Reasoning)
		i, ok := idx[n]
		if !ok {
			if c.Like == "" {
				c.Like = agentdag.CategoryDeep
			}
			out = append(out, Category{Name: n, CategoryConfig: c})
			continue
		}
		have := &out[i].CategoryConfig
		if c.Slot != "" {
			have.Slot = c.Slot
		}
		if c.Tools != "" {
			have.Tools = c.Tools
		}
		if c.Temperature != nil {
			have.Temperature = c.Temperature
		}
		if c.PromptAppend != "" {
			have.PromptAppend = c.PromptAppend
		}
		if c.Description != "" {
			have.Description = c.Description
		}
		if c.Reasoning != "" {
			have.Reasoning = c.Reasoning
		}
	}
	return out, errs
}

func checkCategory(name string, c CategoryConfig) error {
	bad := func(f string, a ...any) error { return fmt.Errorf("ai_categories %q: %s", name, fmt.Sprintf(f, a...)) }
	switch {
	case !agentdag.ValidCategory(name):
		return fmt.Errorf("ai_categories %q: a name is lower-case letters, digits and '-'", name)
	case c.Like != "" && c.Like != agentdag.CategoryQuick && c.Like != agentdag.CategoryDeep:
		return bad("like must be quick or deep")
	case c.Tools != "" && c.Tools != agentdag.PresetNone && c.Tools != agentdag.PresetReadOnly && c.Tools != agentdag.PresetAll:
		return bad("tools must be none, read-only or all")
	case c.Temperature != nil && (*c.Temperature < 0 || *c.Temperature > 2):
		return bad("temperature must be 0 to 2")
	case len([]rune(c.PromptAppend)) > CategoryPromptMax:
		return bad("prompt_append is longer than %d characters", CategoryPromptMax)
	case CheckReasoning(c.Reasoning) != nil:
		return bad("%v", CheckReasoning(c.Reasoning))
	}
	return nil
}

// Escalates reports whether ai_step_escalate is on (the default).
func (s Settings) Escalates() bool { return s.StepEscalate == nil || *s.StepEscalate }

// PlanReviews is ai_plan_review_rounds, 0 to 5 (0: no reviewer).
func (s Settings) PlanReviews() int { return max(0, min(s.PlanReviewRounds, 5)) }

// PlanFiles reports whether a planned run writes its plan file (off
// unless ai_plan_file is true).
func (s Settings) PlanFiles() bool { return s.PlanFile != nil && *s.PlanFile }
