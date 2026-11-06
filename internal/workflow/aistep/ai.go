package aistep

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
)

// DefaultDeny are the tools an ai step never gets in a workflow run
// (plan §6.5): pushes, PRs and comments are workflow write steps, tests
// are the tests step. A step's rule cannot give them back.
var DefaultDeny = []string{"git_push", "run_command", "delete"}

// Step is a workflow `ai` step (plan §3.3) after its templates ran:
//
//	{ "id": "code", "type": "ai", "profile": "fixer" | "model": "quick",
//	  "prompt_file": "prompts/fix.md",
//	  "tools": { "preset": "all", "deny": ["git_push"] },
//	  "approve_plan": true, "budget_usd": 1.0 }
type Step struct {
	// ID is the step id; a Model run's one node has this id.
	ID string
	// Profile is a named profile (pipeline): the run is its nodes.
	Profile string
	// Model is a category (quick | deep) or a model slot name for a
	// one-node run. A step has Profile or Model, not both.
	Model string
	// Prompt is the step's task (prompt or prompt_file, rendered). The
	// item is added after it as a marked block; the engine never puts
	// item text into Prompt itself.
	Prompt string
	// Tools narrows the tools of every node; it can only remove tools,
	// and DefaultDeny is always added.
	Tools agentdag.ToolRule
	// ApprovePlan makes the run wait for the user's answer on its plan
	// (the G-PLAN card) before its first step.
	ApprovePlan bool
	// BudgetUSD (0: none) pauses the item's run before its next node
	// once it cost this much (RunPaused); see also Spend for the whole
	// step.
	BudgetUSD float64
}

// Env is what the app gives the AI steps.
type Env struct {
	// Base are the run options of the app (Build, Emit, Cost, Review,
	// Exec, StallAfter …). The step sets User, Budget, Approval (with
	// ApprovePlan) and Categories, and wraps Build: every node's system
	// prompt gets DataSentence and its tools the step's rule.
	Base agentrun.Options
	// Profile looks a profile up by name.
	Profile func(name string) (ai.Profile, bool)
	// Categories map quick / deep to the app's model slots (a Model run
	// and the nodes a planner adds).
	Categories map[string]string
}

// Rule is the tool rule of step st's nodes: its own rule plus
// DefaultDeny.
func Rule(st Step) agentdag.ToolRule {
	r := st.Tools
	r.Allow = slices.Clone(r.Allow)
	r.Deny = slices.Clone(r.Deny)
	for _, d := range DefaultDeny {
		if !slices.Contains(r.Deny, d) {
			r.Deny = append(r.Deny, d)
		}
	}
	return r
}

// presetRank orders the presets from the most tools to the fewest.
func presetRank(p string) int {
	switch p {
	case agentdag.PresetNone:
		return 2
	case agentdag.PresetReadOnly:
		return 1
	}
	return 0
}

// narrow is rule a narrowed by rule b: the fewer tools of both.
func narrow(a, b agentdag.ToolRule) agentdag.ToolRule {
	out := agentdag.ToolRule{Preset: a.Preset, Deny: slices.Clone(a.Deny)}
	if presetRank(b.Preset) > presetRank(a.Preset) {
		out.Preset = b.Preset
	}
	for _, d := range b.Deny {
		if !slices.Contains(out.Deny, d) {
			out.Deny = append(out.Deny, d)
		}
	}
	switch {
	case len(a.Allow) == 0:
		out.Allow = slices.Clone(b.Allow)
	case len(b.Allow) == 0:
		out.Allow = slices.Clone(a.Allow)
	default:
		for _, x := range a.Allow {
			if slices.Contains(b.Allow, x) {
				out.Allow = append(out.Allow, x)
			}
		}
		if len(out.Allow) == 0 { // nothing both allow: no tools
			out.Preset = agentdag.PresetNone
		}
	}
	return out
}

// Prepare builds the run of step st for item it and the options to
// start it with. Nothing runs yet.
func Prepare(st Step, it Item, runID string, env Env) (agentdag.Run, agentrun.Options, error) {
	o := env.Base
	if o.Build == nil {
		return agentdag.Run{}, o, errors.New("aistep: Env.Base.Build is required")
	}
	if st.BudgetUSD < 0 {
		return agentdag.Run{}, o, fmt.Errorf("step %q: budget_usd must not be negative", st.ID)
	}
	var r agentdag.Run
	budget := st.BudgetUSD
	switch {
	case st.Profile != "" && st.Model != "":
		return r, o, fmt.Errorf("step %q: name a profile or a model, not both", st.ID)
	case st.Profile != "":
		if env.Profile == nil {
			return r, o, fmt.Errorf("step %q: no profiles", st.ID)
		}
		p, ok := env.Profile(st.Profile)
		if !ok {
			return r, o, fmt.Errorf("step %q: no profile %q", st.ID, st.Profile)
		}
		if p.HasCLI() {
			return r, o, fmt.Errorf("step %q: profile %q runs a CLI step; use an agent step for that", st.ID, st.Profile)
		}
		r = p.Run(runID)
		o.Categories = p.CategorySlots()
		if p.BudgetUSD > 0 && (budget == 0 || p.BudgetUSD < budget) {
			budget = p.BudgetUSD
		}
	case st.Model != "":
		o.Categories = env.Categories
		n := agentdag.Node{ID: st.ID, Title: st.ID}
		if st.Model == agentdag.CategoryQuick || st.Model == agentdag.CategoryDeep {
			n.Category, n.Slot = st.Model, env.Categories[st.Model]
		} else {
			n.Slot = st.Model
		}
		r = agentdag.Run{ID: runID, Nodes: []agentdag.Node{n}}
	default:
		return r, o, fmt.Errorf("step %q: name a profile or a model", st.ID)
	}
	rule := Rule(st)
	for i := range r.Nodes {
		r.Nodes[i].Tools = narrow(r.Nodes[i].Tools, rule)
	}
	if r.Loop != nil {
		r.Loop.Fix.Tools = narrow(r.Loop.Fix.Tools, rule)
	}
	o.User = strings.TrimSpace(st.Prompt) + "\n\n" + Block(it)
	o.Budget = budget
	if st.ApprovePlan {
		o.Approval = agentrun.ApprovalAlways
	}
	o.Build = guardBuild(o.Build, rule)
	return r, o, nil
}

// Start prepares step st for item it and starts its run.
func Start(ctx context.Context, st Step, it Item, runID string, env Env) (*agentrun.Handle, error) {
	r, o, err := Prepare(st, it, runID, env)
	if err != nil {
		return nil, err
	}
	return agentrun.Start(ctx, r, o)
}

// guardBuild wraps the app's Build: the agent of every node (nodes a
// planner adds too) keeps only the tools both the node's and the step's
// rule allow, and its system prompt carries DataSentence.
func guardBuild(build func(context.Context, agentdag.Node, string) (*ai.Agent, []ai.Message, error), rule agentdag.ToolRule) func(context.Context, agentdag.Node, string) (*ai.Agent, []ai.Message, error) {
	return func(ctx context.Context, n agentdag.Node, input string) (*ai.Agent, []ai.Message, error) {
		agent, history, err := build(ctx, n, input)
		if err != nil || agent == nil {
			return agent, history, err
		}
		agent.Tools = ai.FilterTools(agent.Tools, narrow(n.Tools, rule))
		history = slices.Clone(history)
		switch {
		case len(history) > 0 && history[0].Role == ai.RoleSystem:
			if !strings.Contains(history[0].Content, DataSentence) {
				history[0].Content += "\n\n" + DataSentence
			}
		default:
			history = append([]ai.Message{{Role: ai.RoleSystem, Content: DataSentence}}, history...)
		}
		return agent, history, nil
	}
}

// Spend is a step's cost over all its items: the engine adds each item
// run's cost and asks Pause before it starts the next item.
type Spend struct {
	mu     sync.Mutex
	budget float64
	cost   float64
	lifted bool
}

// NewSpend is the spend of a step with budget dollars (0: none).
func NewSpend(budget float64) *Spend { return &Spend{budget: budget} }

// Add counts cost dollars.
func (s *Spend) Add(cost float64) {
	s.mu.Lock()
	s.cost += cost
	s.mu.Unlock()
}

// Pause reports whether the step must pause before its next item, with
// the RunPaused event the pause card shows (Cost, Budget). After
// Continue it never pauses again.
func (s *Spend) Pause() (agentrun.Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lifted || s.budget <= 0 || s.cost < s.budget {
		return agentrun.Event{}, false
	}
	return agentrun.Event{Kind: agentrun.RunPaused, Cost: s.cost, Budget: s.budget}, true
}

// Continue lifts the budget for the rest of the step (the pause card's
// Continue).
func (s *Spend) Continue() {
	s.mu.Lock()
	s.lifted = true
	s.mu.Unlock()
}
