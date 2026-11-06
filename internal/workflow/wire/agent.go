package wire

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/aistep"
	"github.com/amin-jalali/termocode/internal/workflow/run"
)

// agentCLI maps a step's agent to the CLI clistep runs.
func agentCLI(agent string) (string, bool) {
	switch agent {
	case "codex":
		return ai.CLICodex, true
	case "claude-code":
		return ai.CLIClaude, true
	}
	return "", false
}

// runAgent runs an agent step (WFX-11): Claude Code or Codex through
// clistep, on the user's own login, in the item's worktree. Sandbox
// "worktree" lets it change files there and nowhere else (Codex:
// workspace-write on that folder; Claude Code: file tools, no shell),
// so it can neither push nor open a PR: those are write steps. Sandbox
// "read-only" only reads. The prompt and the item go on stdin, never in
// argv; the environment loses every secret (clistep.CleanEnv).
func (s *Steps) runAgent(ctx context.Context, c run.RunCall) (run.Output, error) {
	sp := c.Step.Agent
	if sp == nil {
		return run.Output{}, run.Permanent(errors.New("the step has no agent"))
	}
	cli, ok := agentCLI(sp.Agent)
	if !ok {
		return run.Output{}, run.Permanent(fmt.Errorf("unknown agent %q", sp.Agent))
	}
	sandbox := sp.Sandbox
	if sandbox == "" {
		sandbox = s.AgentSandbox
	}
	if sandbox == "" {
		sandbox = workflow.DefaultAgentSandbox
	}
	write := sandbox == workflow.SandboxWorktree
	dir := c.Env.Worktree
	if dir == "" {
		if write {
			return run.Output{}, run.Permanent(errors.New(`an agent step that changes files runs only in an item's worktree: give its each "worktree": true, or use "sandbox": "read-only"`))
		}
		dir = s.Workspace
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
	key := ""
	if c.Item != nil {
		it := aiItem(*c.Item)
		key = it.Key
		if chip := aistep.Scan(it.Text); chip.Flagged() {
			yes := false
			if s.AllowFlagged != nil {
				var err error
				if yes, err = s.AllowFlagged(ctx, c.Step.ID, key, chip); err != nil {
					return run.Output{}, err
				}
			}
			if !yes {
				return run.Output{}, fmt.Errorf("item %s %s (%s); the agent did not get it: %w",
					key, aistep.ChipText, strings.Join(chip.Reasons, ", "), run.ErrDenied)
			}
		}
		prompt = strings.TrimSpace(prompt) + "\n\n" + aistep.DataSentence + "\n\n" + aistep.Block(it) + "\n"
	}

	spec := clistep.Spec{CLI: cli, Write: write, Model: sp.Model, Dir: dir, Env: s.AgentEnv}
	runCLI := s.Agent
	if runCLI == nil {
		runCLI = clistep.Run
	}
	emit := func(e ai.Event) {
		if s.OnAgent != nil {
			s.OnAgent(c.Step.ID, key, e)
		}
	}
	res, err := runCLI(ctx, spec, prompt, emit)
	out := run.Output{Text: res.Text, Worktree: c.Env.Worktree}
	if len(res.Files) > 0 {
		files := make([]any, len(res.Files))
		for i, f := range res.Files {
			files[i] = f
		}
		out.JSON = map[string]any{"files": files}
	}
	var limit *ai.LimitError
	switch {
	case err == nil:
		return out, nil
	case errors.As(err, &limit):
		return out, fmt.Errorf("%w: %w", err, run.ErrPaused)
	case ctx.Err() != nil:
		return out, err
	case strings.Contains(err.Error(), "not installed") || strings.Contains(err.Error(), "not logged in"):
		return out, run.Permanent(err)
	}
	return out, err
}
