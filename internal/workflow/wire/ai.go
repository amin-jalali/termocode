package wire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agentenv"
	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/tasks"
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
	// TestArgv is the test command when the project has no test task (a
	// tests step without argv).
	TestArgv []string
	// Tasks lists the project's tasks in dir (nil: tasks.json and the
	// detected ones, internal/tasks).
	Tasks func(dir string) []tasks.Task
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
	// AgentEnvFile is the project's trusted .termocode/agent-env.json
	// (nil: none): setup once per worktree, plain values, setup secrets
	// from AgentSecrets, the network rule (CMP-63, agentenv.go).
	AgentEnvFile *agentenv.Env
	AgentSecrets agentenv.Getter
	// OnNetwork hears the hosts the network rule blocked in one agent
	// run; may be nil.
	OnNetwork func(step, item string, blocked []string)

	setupMu   sync.Mutex
	setupRuns map[string]*setupRun
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
	case workflow.TypePipeline:
		sp := c.Step.Pipeline
		if sp == nil || sp.Profile == "" {
			return run.Output{}, run.Permanent(errors.New("a pipeline step needs a profile"))
		}
		return s.runAgentRun(ctx, c, aistep.Step{ID: c.Step.ID, Profile: sp.Profile}, sp.PromptFile)
	case workflow.TypeShell:
		if c.Step.Shell == nil {
			return run.Output{}, run.Permanent(errors.New("the step has no argv"))
		}
		if len(c.Step.Shell.Env) > 0 {
			return run.Output{}, run.Permanent(errors.New("shell: env is not supported yet"))
		}
		return s.exec(ctx, c, c.Step.Shell.Argv, c.Step.Shell.Cwd)
	case workflow.TypeTests:
		argv, cwd, err := s.testArgv(c)
		if err != nil {
			return run.Output{}, err
		}
		return s.exec(ctx, c, argv, cwd)
	}
	return run.Output{}, run.Permanent(fmt.Errorf("%s steps: %w", c.Step.Type, errNotYet))
}

// testArgv is what a tests step runs: its argv, else the project's task
// (by label; "" or "test": the default test task) through sh -c in the
// item's folder, else TestArgv.
func (s *Steps) testArgv(c run.RunCall) (argv []string, cwd string, err error) {
	sp := c.Step.Tests
	if sp != nil && len(sp.Argv) > 0 {
		return sp.Argv, "", nil
	}
	name := ""
	if sp != nil {
		name = sp.Task
	}
	dir := s.Workspace
	if c.Env.Worktree != "" {
		dir = c.Env.Worktree
	}
	list := s.Tasks
	if list == nil {
		list = projectTasks
	}
	all := list(dir)
	pick := findTask(all, name)
	if pick == nil {
		if (name == "" || name == "test") && len(s.TestArgv) > 0 {
			return s.TestArgv, "", nil
		}
		var labels []string
		for _, t := range all {
			labels = append(labels, t.Label)
		}
		if len(labels) == 0 {
			return nil, "", run.Permanent(errors.New("tests: the project has no test task; give the step an argv"))
		}
		return nil, "", run.Permanent(fmt.Errorf("tests: no task %q; the project has: %s", name, strings.Join(labels, ", ")))
	}
	if len(pick.Env) > 0 {
		return nil, "", run.Permanent(fmt.Errorf("tests: task %q sets env, which a workflow cannot pass yet; give the step an argv", pick.Label))
	}
	v := tasks.Vars{WorkspaceFolder: dir}
	if d := pick.Dir(v); d != dir {
		rel, rerr := filepath.Rel(dir, d)
		if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, "", run.Permanent(fmt.Errorf("tests: task %q runs outside the workspace", pick.Label))
		}
		cwd = rel
	}
	return []string{"sh", "-c", pick.CommandLine(v)}, cwd, nil
}

// findTask is the task labelled name; for "" or "test" without one, the
// default test task (else the first test task). nil: none.
func findTask(all []tasks.Task, name string) *tasks.Task {
	for i := range all {
		if all[i].Label == name {
			return &all[i]
		}
	}
	if name != "" && name != "test" {
		return nil
	}
	var first *tasks.Task
	for i := range all {
		if all[i].Group != tasks.GroupTest {
			continue
		}
		if all[i].IsDefault {
			return &all[i]
		}
		if first == nil {
			first = &all[i]
		}
	}
	return first
}

// projectTasks is tasks.json (when it reads) and the detected tasks.
func projectTasks(dir string) []tasks.Task {
	list, _ := tasks.Load(dir)
	return append(list, tasks.Detect(dir)...)
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
	st := aistep.Step{ID: c.Step.ID, Profile: sp.Profile, Model: sp.Model,
		ApprovePlan: sp.ApprovePlan, BudgetUSD: sp.BudgetUSD}
	if sp.Tools != nil {
		st.Tools = agentdag.ToolRule{Preset: sp.Tools.Preset, Deny: sp.Tools.Deny}
	}
	return s.runAgentRun(ctx, c, st, sp.PromptFile)
}

// runAgentRun runs an ai or pipeline step as one agentrun run (the
// step's prompt, or its prompt file filled).
func (s *Steps) runAgentRun(ctx context.Context, c run.RunCall, st aistep.Step, promptFile string) (run.Output, error) {
	prompt := c.Prompt
	if promptFile != "" {
		b, err := s.readPrompt(promptFile)
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
	st.Prompt = prompt
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
