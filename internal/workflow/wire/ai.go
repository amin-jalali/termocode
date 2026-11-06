package wire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/aistep"
	"github.com/amin-jalali/termocode/internal/workflow/run"
)

// Steps runs plan steps (run.Planner) and run steps (run.Runner): ai
// through aistep, shell and tests through Exec.
type Steps struct {
	// Env is what the app gives the AI steps.
	Env aistep.Env
	// RunID names the workflow run (the AI runs are named after it).
	RunID string
	// Dir is the workflow file's folder; prompt files stay inside it.
	Dir string
	// Workspace is where shell and tests steps run outside an each.
	Workspace string
	// Exec runs argv in dir (internal/spawn in the app): its output and
	// exit code. nil: shell and tests steps fail.
	Exec func(ctx context.Context, argv []string, dir string) (out string, exit int, err error)
	// TestArgv is the project's test task (a tests step without argv).
	TestArgv []string
	// OnEvent gets the AI runs' events (live output); may be nil.
	OnEvent func(step, item string, e agentrun.Event)

	// Agent runs an outside agent CLI (nil: clistep.Run).
	Agent func(ctx context.Context, s clistep.Spec, prompt string, emit func(ai.Event)) (clistep.Result, error)
	// AgentEnv is the agents' environment before CleanEnv (nil: the
	// user's, with termocode's tools first).
	AgentEnv []string
	// AgentSandbox is workflows_agent_sandbox ("": worktree).
	AgentSandbox string
	// AllowFlagged asks the user before an agent works on an item that
	// looks like an injection (nil: the item is skipped).
	AllowFlagged func(ctx context.Context, step, item string, chip aistep.Chip) (bool, error)
	// OnAgent gets the agents' events (live output); may be nil.
	OnAgent func(step, item string, e ai.Event)
}

// Plan runs a plan step over every item.
func (s *Steps) Plan(ctx context.Context, c run.PlanCall) (run.PlanResult, error) {
	sp := c.Step.Plan
	if sp == nil {
		return run.PlanResult{}, run.Permanent(errors.New("the step has no plan"))
	}
	prompt := c.Prompt
	if sp.PromptFile != "" {
		b, err := s.readPrompt(sp.PromptFile)
		if err != nil {
			return run.PlanResult{}, err
		}
		prompt = string(b)
	}
	model := sp.Model
	if model == "" {
		model = sp.Profile
	}
	items := make([]aistep.Item, len(c.Items))
	for i, it := range c.Items {
		items[i] = aiItem(it)
	}
	env := s.env(c.Step.ID, "", nil)
	p, err := aistep.RunPlan(ctx, aistep.PlanStep{ID: c.Step.ID, Model: model, Prompt: prompt}, items, s.RunID+"-"+c.Step.ID, env)
	if err != nil {
		return run.PlanResult{}, err
	}
	return run.PlanResult{Order: p.Order, Skip: p.Skip, Labels: p.Labels}, nil
}

// Run runs one run step.
func (s *Steps) Run(ctx context.Context, c run.RunCall) (run.Output, error) {
	switch c.Step.Type {
	case workflow.TypeAI:
		return s.runAI(ctx, c)
	case workflow.TypeAgent:
		return s.runAgent(ctx, c)
	case workflow.TypeShell:
		if c.Step.Shell == nil {
			return run.Output{}, run.Permanent(errors.New("the step has no argv"))
		}
		if len(c.Step.Shell.Env) > 0 {
			return run.Output{}, run.Permanent(errors.New("shell: env is not supported yet"))
		}
		return s.exec(ctx, c, c.Step.Shell.Argv, c.Step.Shell.Cwd)
	case workflow.TypeTests:
		argv := s.TestArgv
		if c.Step.Tests != nil && len(c.Step.Tests.Argv) > 0 {
			argv = c.Step.Tests.Argv
		}
		if len(argv) == 0 {
			return run.Output{}, run.Permanent(errors.New("tests: the project has no test task; give the step an argv"))
		}
		return s.exec(ctx, c, argv, "")
	}
	return run.Output{}, run.Permanent(fmt.Errorf("%s steps: %w", c.Step.Type, errNotYet))
}

func (s *Steps) exec(ctx context.Context, c run.RunCall, argv []string, cwd string) (run.Output, error) {
	if s.Exec == nil {
		return run.Output{}, run.Permanent(errors.New("commands cannot run here"))
	}
	dir := s.Workspace
	if c.Env.Worktree != "" {
		dir = c.Env.Worktree
	}
	if cwd != "" && cwd != "." {
		rel := filepath.Clean(filepath.FromSlash(cwd))
		if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return run.Output{}, run.Permanent(fmt.Errorf("cwd %q is not inside the workspace", cwd))
		}
		dir = filepath.Join(dir, rel)
	}
	out, code, err := s.Exec(ctx, argv, dir)
	o := run.Output{Text: out, Exit: code, Worktree: c.Env.Worktree}
	if err != nil {
		return o, err
	}
	if code != 0 {
		return o, fmt.Errorf("%s exited with %d", filepath.Base(argv[0]), code)
	}
	return o, nil
}

func (s *Steps) runAI(ctx context.Context, c run.RunCall) (run.Output, error) {
	sp := c.Step.AI
	if sp == nil {
		return run.Output{}, run.Permanent(errors.New("the step has no ai spec"))
	}
	prompt := c.Prompt
	if sp.PromptFile != "" {
		b, err := s.readPrompt(sp.PromptFile)
		if err != nil {
			return run.Output{}, err
		}
		prompt = string(b)
		if c.Render != nil {
			if prompt, err = c.Render(prompt); err != nil {
				return run.Output{}, run.Permanent(fmt.Errorf("prompt_file: %w", err))
			}
		}
	}
	st := aistep.Step{ID: c.Step.ID, Profile: sp.Profile, Model: sp.Model, Prompt: prompt,
		ApprovePlan: sp.ApprovePlan, BudgetUSD: sp.BudgetUSD}
	if sp.Tools != nil {
		st.Tools = agentdag.ToolRule{Preset: sp.Tools.Preset, Deny: sp.Tools.Deny}
	}
	var it aistep.Item
	key := ""
	if c.Item != nil {
		it, key = aiItem(*c.Item), c.Item.Key
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	paused := make(chan struct{}, 1)
	env := s.env(c.Step.ID, key, func(e agentrun.Event) {
		if e.Kind == agentrun.RunPaused {
			select {
			case paused <- struct{}{}:
			default:
			}
			cancel()
		}
	})
	h, err := aistep.Start(ctx, st, it, runName(s.RunID, c.Step.ID, key), env)
	if err != nil {
		return run.Output{}, run.Permanent(err)
	}
	ev := h.Wait()
	select {
	case <-paused:
		return run.Output{CostUSD: ev.Cost}, fmt.Errorf("the step reached its budget: %w", run.ErrPaused)
	default:
	}
	switch ev.Kind {
	case agentrun.RunDone:
		return run.Output{Text: ev.Output, CostUSD: ev.Cost, Worktree: c.Env.Worktree}, nil
	case agentrun.RunCancelled:
		return run.Output{CostUSD: ev.Cost}, context.Canceled
	}
	msg := ev.Err
	if msg == "" {
		msg = "the AI step failed"
	}
	return run.Output{CostUSD: ev.Cost}, errors.New(msg)
}

// env is the app's Env with Emit wrapped: the run's events go to
// OnEvent and to watch.
func (s *Steps) env(step, item string, watch func(agentrun.Event)) aistep.Env {
	env := s.Env
	base := env.Base.Emit
	env.Base.Emit = func(e agentrun.Event) {
		if base != nil {
			base(e)
		}
		if s.OnEvent != nil {
			s.OnEvent(step, item, e)
		}
		if watch != nil {
			watch(e)
		}
	}
	return env
}

func (s *Steps) readPrompt(rel string) ([]byte, error) {
	if s.Dir == "" {
		return nil, run.Permanent(errors.New("prompt_file: no workflow folder"))
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, run.Permanent(fmt.Errorf("prompt_file %q is not inside the workflow folder", rel))
	}
	b, err := os.ReadFile(filepath.Join(s.Dir, clean))
	if err != nil {
		return nil, run.Permanent(fmt.Errorf("prompt_file: %w", err))
	}
	if len(b) > workflow.MaxPromptBytes {
		return nil, run.Permanent(fmt.Errorf("prompt_file %q is over %d KB", rel, workflow.MaxPromptBytes>>10))
	}
	return b, nil
}

func aiItem(it run.Item) aistep.Item {
	text, ok := it.Data.(string)
	if !ok {
		b, _ := json.MarshalIndent(it.Data, "", "  ")
		text = string(b)
	}
	return aistep.Item{Key: it.Key, Source: "workflow", Text: text}
}

func runName(runID, step, item string) string {
	n := runID + "-" + step
	if item != "" {
		n += "-" + item
	}
	return n
}
