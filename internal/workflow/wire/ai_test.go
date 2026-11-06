package wire

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/aistep"
	"github.com/amin-jalali/termocode/internal/workflow/run"
)

var (
	_ run.Planner = (*Steps)(nil)
	_ run.Runner  = (*Steps)(nil)
)

type replyProv struct {
	reply string
	mu    sync.Mutex
	users []string
}

func (p *replyProv) Name() string        { return "fake" }
func (p *replyProv) Configured() bool    { return true }
func (p *replyProv) SupportsTools() bool { return true }
func (p *replyProv) Stream(_ context.Context, req ai.Request) <-chan ai.Chunk {
	p.mu.Lock()
	var b strings.Builder
	for _, m := range req.Messages {
		if m.Role == ai.RoleUser {
			b.WriteString(m.Content)
		}
	}
	p.users = append(p.users, b.String())
	p.mu.Unlock()
	ch := make(chan ai.Chunk, 2)
	ch <- ai.Chunk{Delta: p.reply}
	ch <- ai.Chunk{Done: true}
	close(ch)
	return ch
}

func stepsEnv(p *replyProv) aistep.Env {
	return aistep.Env{
		Base: agentrun.Options{Build: func(_ context.Context, n agentdag.Node, input string) (*ai.Agent, []ai.Message, error) {
			return &ai.Agent{Provider: p, Native: true}, []ai.Message{{Role: ai.RoleSystem, Content: "sys"}, {Role: ai.RoleUser, Content: input}}, nil
		}},
		Categories: map[string]string{agentdag.CategoryQuick: "quick", agentdag.CategoryDeep: "main"},
	}
}

// Spec (WFX wiring): a plan step runs aistep's plan over every item and
// maps its answer by key.
func TestStepsPlan(t *testing.T) {
	p := &replyProv{reply: "```json\n{\"order\":[\"B\",\"A\"],\"skip\":{\"A\":\"no repro\"}}\n```"}
	s := &Steps{Env: stepsEnv(p), RunID: "r1"}
	st := &workflow.Step{ID: "order", Type: workflow.TypePlan, Plan: &workflow.PlanSpec{Model: "quick"}}
	res, err := s.Plan(context.Background(), run.PlanCall{Step: st, Items: []run.Item{{Key: "A", Data: map[string]any{"t": "a"}}, {Key: "B", Data: "b"}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Order, ",") != "B,A" || res.Skip["A"] != "no repro" {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(p.users[0], "key=A") {
		t.Fatalf("items go as marked blocks: %q", p.users[0])
	}
}

// Spec: an ai step runs our AI with the prompt file filled and the item
// as a data block after it; its answer is steps.<id>.text.
func TestStepsRunAI(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fix.md"), []byte("Fix {{item.title}}."), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &replyProv{reply: "done it"}
	s := &Steps{Env: stepsEnv(p), RunID: "r1", Dir: dir}
	st := &workflow.Step{ID: "code", Type: workflow.TypeAI, AI: &workflow.AISpec{Model: "quick", PromptFile: "fix.md"}}
	it := &run.Item{Key: "LIN-1", Data: map[string]any{"title": "crash"}}
	render := func(tmpl string) (string, error) { return strings.ReplaceAll(tmpl, "{{item.title}}", "crash"), nil }
	out, err := s.Run(context.Background(), run.RunCall{Step: st, Item: it, Render: render})
	if err != nil || out.Text != "done it" {
		t.Fatalf("%+v %v", out, err)
	}
	if u := p.users[0]; !strings.HasPrefix(u, "Fix crash.") || !strings.Contains(u, "key=LIN-1") {
		t.Fatalf("prompt %q", u)
	}
	bad := &workflow.Step{ID: "x", Type: workflow.TypeAI, AI: &workflow.AISpec{Model: "quick", PromptFile: "../../etc/passwd"}}
	if _, err := s.Run(context.Background(), run.RunCall{Step: bad, Item: it, Render: render}); err == nil {
		t.Fatal("a prompt file outside the workflow folder")
	}
}

// Spec: shell and tests steps run argv through Exec in the item's
// worktree (or the workspace); a non-zero exit fails the step with the
// exit code kept; agent and pipeline steps fail clearly for now.
func TestStepsRunShell(t *testing.T) {
	var dirs []string
	s := &Steps{Workspace: "/ws", Exec: func(_ context.Context, argv []string, dir string) (string, int, error) {
		dirs = append(dirs, dir)
		if argv[0] == "false" {
			return "boom", 1, nil
		}
		return "ok " + strings.Join(argv[1:], " "), 0, nil
	}, TestArgv: []string{"go", "test", "./..."}}
	out, err := s.Run(context.Background(), run.RunCall{Step: &workflow.Step{ID: "fmt", Type: workflow.TypeShell, Shell: &workflow.ShellSpec{Argv: []string{"gofmt", "-l", "."}, Cwd: "sub"}}})
	if err != nil || out.Text != "ok -l ." || dirs[0] != filepath.Join("/ws", "sub") {
		t.Fatalf("%+v %v %v", out, err, dirs)
	}
	out, err = s.Run(context.Background(), run.RunCall{Step: &workflow.Step{ID: "t", Type: workflow.TypeTests, Tests: &workflow.TestsSpec{Task: "test"}}, Env: run.ItemEnv{Worktree: "/wt"}})
	if err != nil || out.Text != "ok test ./..." || dirs[1] != "/wt" {
		t.Fatalf("%+v %v %v", out, err, dirs)
	}
	out, err = s.Run(context.Background(), run.RunCall{Step: &workflow.Step{ID: "f", Type: workflow.TypeShell, Shell: &workflow.ShellSpec{Argv: []string{"false"}}}})
	if err == nil || out.Exit != 1 {
		t.Fatalf("exit 1 fails: %+v %v", out, err)
	}
	for _, typ := range []string{workflow.TypeAgent, workflow.TypePipeline} {
		if _, err := s.Run(context.Background(), run.RunCall{Step: &workflow.Step{ID: "a", Type: typ}}); err == nil || errors.Is(err, run.ErrDenied) {
			t.Errorf("%s: %v", typ, err)
		}
	}
}
