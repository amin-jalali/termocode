package wire

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/tasks"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/run"
)

// WFX-12: a pipeline step runs a named profile through agentrun (the
// same runs as the Runs view), with the item as a data block; a tests
// step runs the project's test task (internal/tasks) in the item's
// worktree, or its argv, and fails on red so its else runs.

func TestPipelineStepRunsTheProfile(t *testing.T) {
	p := &replyProv{reply: "reviewed and fixed"}
	env := stepsEnv(p)
	env.Profile = func(name string) (ai.Profile, bool) {
		if name == "review-then-fix" {
			return ai.Profile{Name: name, Steps: []ai.ProfileStep{{ID: "review"}, {ID: "fix"}}}, true
		}
		return ai.Profile{}, false
	}
	s := &Steps{Env: env, RunID: "r1"}
	st := &workflow.Step{ID: "pipe", Type: workflow.TypePipeline, Pipeline: &workflow.PipelineSpec{Profile: "review-then-fix"}}
	it := &run.Item{Key: "PR-9", Data: map[string]any{"title": "flaky test"}}
	out, err := s.Run(context.Background(), run.RunCall{Step: st, Item: it, Prompt: "Review {{item.title}}", Env: run.ItemEnv{Worktree: "/wt"}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "reviewed and fixed" || out.Worktree != "/wt" {
		t.Errorf("output %+v", out)
	}
	p.mu.Lock()
	n, first := len(p.users), p.users[0]
	p.mu.Unlock()
	if n != 2 {
		t.Errorf("%d model calls, want one per profile step", n)
	}
	if !strings.Contains(first, "key=PR-9") {
		t.Errorf("the item is not a data block: %q", first)
	}

	st.Pipeline.Profile = "nope"
	if _, err := s.Run(context.Background(), run.RunCall{Step: st, Item: it}); err == nil {
		t.Error("an unknown profile ran")
	}
}

func TestTestsStepUsesTheProjectTask(t *testing.T) {
	var got [][]string
	var dirs []string
	s := &Steps{Workspace: "/ws",
		Exec: func(_ context.Context, argv []string, dir string) (string, int, error) {
			got, dirs = append(got, argv), append(dirs, dir)
			if strings.Contains(strings.Join(argv, " "), "lint") {
				return "lint: 2 problems", 1, nil
			}
			return "ok", 0, nil
		},
		Tasks: func(dir string) []tasks.Task {
			return []tasks.Task{
				{Label: "go: build", Command: "go build ./...", Group: tasks.GroupBuild},
				{Label: "go: test", Command: "go test ./...", Group: tasks.GroupTest},
				{Label: "unit", Command: "make unit", Args: []string{"-j 2"}, Group: tasks.GroupTest, IsDefault: true},
				{Label: "npm: lint", Command: "npm run lint"},
			}
		}}
	step := func(task string) *workflow.Step {
		return &workflow.Step{ID: "test", Type: workflow.TypeTests, Tests: &workflow.TestsSpec{Task: task}}
	}
	// "test" is the default test task; it runs in the item's worktree.
	if _, err := s.Run(context.Background(), run.RunCall{Step: step("test"), Env: run.ItemEnv{Worktree: "/wt"}}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"sh", "-c", "make unit '-j 2'"}; !reflect.DeepEqual(got[0], want) || dirs[0] != "/wt" {
		t.Errorf("argv %q in %s", got[0], dirs[0])
	}
	// A task by its label; red fails with the exit code and the output.
	out, err := s.Run(context.Background(), run.RunCall{Step: step("npm: lint")})
	if err == nil || out.Exit != 1 || out.Text != "lint: 2 problems" {
		t.Errorf("red: %+v %v", out, err)
	}
	if !reflect.DeepEqual(got[1], []string{"sh", "-c", "npm run lint"}) || dirs[1] != "/ws" {
		t.Errorf("argv %q in %s", got[1], dirs[1])
	}
	// An unknown task names the ones there are.
	_, err = s.Run(context.Background(), run.RunCall{Step: step("nope")})
	if err == nil || !strings.Contains(err.Error(), "go: test") {
		t.Errorf("unknown task: %v", err)
	}
}

func TestTestsStepWithoutTasksFallsBackToTestArgv(t *testing.T) {
	var got []string
	s := &Steps{Workspace: "/ws", TestArgv: []string{"go", "test", "./..."},
		Exec:  func(_ context.Context, argv []string, _ string) (string, int, error) { got = argv; return "", 0, nil },
		Tasks: func(string) []tasks.Task { return nil }}
	if _, err := s.Run(context.Background(), run.RunCall{Step: &workflow.Step{ID: "t", Type: workflow.TypeTests, Tests: &workflow.TestsSpec{}}}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, " ") != "go test ./..." {
		t.Errorf("argv %q", got)
	}
}
