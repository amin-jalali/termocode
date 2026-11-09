package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
)

// AI parity (agent runs): agent_runs does what the Runs view and the
// agent worktrees do. List, output, pause and resume have no card in the
// Runs view; Merge and Discard of an agent worktree are cards on the
// page, so they ask every time with no Always allow.

func aiAgentRunsRun(t *testing.T, m *Model, args map[string]any) (string, error) {
	t.Helper()
	call := func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(m)
		return out, err
	}
	tl, ok := ai.FindTool(m.aiChatTools(aiWorkspaceRoot(), call), "agent_runs")
	if !ok {
		t.Fatal("no agent_runs tool in the chat")
	}
	if err := tl.Validate(args); err != nil {
		return "", err
	}
	return tl.Run(context.Background(), args)
}

func TestAIAgentRunsGateMatchesTheGUI(t *testing.T) {
	m := runsRig(t, "list", 200, 50)
	tl, ok := ai.FindTool(m.aiChatTools(aiWorkspaceRoot(), nil), "agent_runs")
	if !ok {
		t.Fatal("no agent_runs tool in the chat")
	}
	for op, ask := range map[string]bool{"list": false, "output": false, "pause": false, "resume": false, "merge": true, "discard": true} {
		if got, class := tl.Gate(map[string]any{"op": op}); got != ask || class != "" {
			t.Errorf("op %s: ask=%v class=%q, want ask=%v and no Always allow", op, got, class, ask)
		}
	}
	if err := tl.Validate(map[string]any{"op": "merge", "worktree": "../x"}); err == nil {
		t.Error("a bad worktree id passed")
	}
	if card := tl.Describe(map[string]any{"op": "discard", "worktree": "old-y"}); !strings.Contains(card, "cannot be undone") || !strings.Contains(card, git.AgentBranchPrefix+"old-y") {
		t.Errorf("discard card: %s", card)
	}
	if r, ok := mcpServeRow("agent_runs"); !ok || strings.Join(r.only, ",") != "list,output" {
		t.Errorf("outside agents get agent_runs %v (%v); want its reads only: list, output", r.only, ok)
	}
}

// list names every chat's runs with their key and state; output reads
// one run's steps; pause and resume act on the open chat's run.
func TestAIAgentRunsListOutputPauseResume(t *testing.T) {
	m := runsRig(t, "list", 200, 50)
	out, err := aiAgentRunsRun(t, m, map[string]any{"op": "list"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"running", `"fix the toast flicker"`, "(this chat)", "resumable", `"write release notes for 0.9"`, "done"} {
		if !strings.Contains(out, want) {
			t.Errorf("list misses %q:\n%s", want, out)
		}
	}
	done := runsItemByTask(t, m, "why does the toast flicker")
	out, err = aiAgentRunsRun(t, m, map[string]any{"op": "output", "key": done.key})
	if err != nil || !strings.Contains(out, "Step 1") || !strings.Contains(out, done.key) {
		t.Fatalf("output: %q %v", out, err)
	}
	if _, err := aiAgentRunsRun(t, m, map[string]any{"op": "pause", "key": done.key}); err == nil || !strings.Contains(err.Error(), "going") {
		t.Errorf("pause of a done run: %v", err)
	}
	notes := runsItemByTask(t, m, "write release notes for 0.9")
	if _, err := aiAgentRunsRun(t, m, map[string]any{"op": "resume", "key": notes.key}); err == nil || !strings.Contains(err.Error(), "Runs view") {
		t.Errorf("resume of another chat's stopped run: %v", err)
	}
	if _, err := aiAgentRunsRun(t, m, map[string]any{"op": "output", "key": "no/such"}); err == nil {
		t.Error("an unknown key was read")
	}
}

// pause holds the open chat's live run before its next step and resume
// lets it go on (p and r in the Runs view); a stopped run resumes only as
// the last turn of its chat, which the AI's own turn is now: the model is
// told the user presses r.
func TestAIAgentRunsPauseResumeLiveRun(t *testing.T) {
	m := runsRig(t, "list", 200, 50)
	live := runsItemByTask(t, m, "fix the toast flicker")
	m.ai.run = &agentrun.Handle{} // the demo's live run, in front
	if out, err := aiAgentRunsRun(t, m, map[string]any{"op": "pause", "key": live.key}); err != nil || !m.ai.runPausing {
		t.Fatalf("pause: %q %v pausing %v", out, err, m.ai.runPausing)
	}
	if out, err := aiAgentRunsRun(t, m, map[string]any{"op": "resume", "key": live.key}); err != nil || m.ai.runPausing {
		t.Fatalf("resume: %q %v pausing %v", out, err, m.ai.runPausing)
	}
	m.ai.streaming, m.ai.run = false, nil // the open chat's run was stopped
	m.ai.conv.Messages[1].Run.State = ai.RunStateCancelled
	stopped := runsItemByTask(t, m, "fix the toast flicker")
	if stopped.kind != runsResumable {
		t.Fatalf("the stopped run is %v", stopped.kind)
	}
	if _, err := aiAgentRunsRun(t, m, map[string]any{"op": "resume", "key": stopped.key}); err == nil || !strings.Contains(err.Error(), "last turn") {
		t.Errorf("resume of a stopped run: %v", err)
	}
}

// merge writes the agent's work into the user's files and closes the
// worktree; discard removes the worktree and its branch.
func TestAIAgentRunsMergeAndDiscard(t *testing.T) {
	r, dir := webAIWorktreeRig(t)
	m := r.m
	out, err := aiAgentRunsRun(t, m, map[string]any{"op": "merge", "worktree": "old-y"})
	if err != nil || !strings.Contains(out, "Merged 1 file(s)") || !strings.Contains(out, "y.txt") {
		t.Fatalf("merge: %q %v", out, err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "y.txt")); err != nil || string(b) != "y\n" {
		t.Fatalf("y.txt after the merge: %q %v", b, err)
	}
	out, err = aiAgentRunsRun(t, m, map[string]any{"op": "discard", "worktree": "old-x"})
	if err != nil || !strings.Contains(out, "Discarded") {
		t.Fatalf("discard: %q %v", out, err)
	}
	left, _ := git.LeftoverAgentWorktrees(dir)
	if len(left) != 0 {
		t.Errorf("worktrees left: %+v", left)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "mine\n" {
		t.Errorf("discard changed the user's a.txt: %q", b)
	}
	if _, err := aiAgentRunsRun(t, m, map[string]any{"op": "discard", "worktree": "old-x"}); err == nil {
		t.Error("a gone worktree was discarded again")
	}
}
