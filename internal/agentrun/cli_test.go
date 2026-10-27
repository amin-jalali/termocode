package agentrun

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// CS-1/2/4: a node with a CLI runs through Options.Exec (never an
// ai.Agent); its events, output and usage flow like an agent node's; a
// CLI verify node ends with a verdict; a usage limit pauses the run at
// that node and Continue runs it again.

// fakeExec answers each node's calls with the next scripted reply.
type fakeExec struct {
	mu      sync.Mutex
	replies map[string][]execReply
	calls   map[string][]string // node → inputs
}

type execReply struct {
	text string
	err  error
}

func (f *fakeExec) exec(_ context.Context, n agentdag.Node, input string, emit func(ai.Event)) (string, *ai.Usage, error) {
	f.mu.Lock()
	k := len(f.calls[n.ID])
	f.calls[n.ID] = append(f.calls[n.ID], input)
	rs := f.replies[n.ID]
	r := execReply{text: "ok"}
	if len(rs) > 0 {
		r = rs[min(k, len(rs)-1)]
	}
	f.mu.Unlock()
	emit(ai.Event{Kind: ai.EventToolStart, Tool: "Read", Detail: "Read a.go"})
	emit(ai.Event{Kind: ai.EventToolResult, Tool: "Read", Detail: "3 lines"})
	emit(ai.Event{Kind: ai.EventText, Text: r.text})
	return r.text, &ai.Usage{Model: n.CLI, Input: 10, Output: 5, Requests: 1}, r.err
}

func (f *fakeExec) inputs(id string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls[id]...)
}

func cliOptions(h *harness, f *fakeExec) Options {
	o := planOptions(h, 1)
	o.Exec = f.exec
	return o
}

func newFakeExec(r map[string][]execReply) *fakeExec {
	return &fakeExec{replies: r, calls: map[string][]string{}}
}

func TestCLINodeRunsThroughExec(t *testing.T) {
	h := newHarness(map[string]*fakeProv{"b": {replies: []ai.Chunk{{Delta: "b done"}}}})
	f := newFakeExec(map[string][]execReply{"a": {{text: "claude did the work"}}})
	r := chain("a", "b")
	r.Nodes[0].CLI = ai.CLIClaude
	run, err := Start(context.Background(), r, cliOptions(h, f))
	if err != nil {
		t.Fatal(err)
	}
	end := wait(t, run)
	if end.Kind != RunDone || end.Output != "b done" {
		t.Fatalf("end %+v", end)
	}
	if in := f.inputs("a"); len(in) != 1 || !strings.Contains(in[0], "fix the bug") {
		t.Errorf("a's input %q", in)
	}
	if in := h.input("b"); !strings.Contains(in, "### Output of step \"A\"\nclaude did the work") {
		t.Errorf("b's input:\n%s", in)
	}
	if _, built := h.inputs["a"]; built {
		t.Error("a CLI node must not build an agent")
	}
	done, _ := h.find(NodeDone, "a")
	if done.Output != "claude did the work" || done.Usage == nil || done.Usage.Model != ai.CLIClaude {
		t.Errorf("a done %+v", done)
	}
	if got := h.kinds("a"); len(got) < 4 || got[1] != NodeTool || got[2] != NodeTool || got[3] != NodeText {
		t.Errorf("a's events %v", got)
	}
	if end.Usage == nil || end.Usage.Input != 10 {
		t.Errorf("run usage %+v", end.Usage)
	}
}

func TestCLINodeWithoutExecFails(t *testing.T) {
	h := newHarness(nil)
	r := chain("a")
	r.Nodes[0].CLI = ai.CLICodex
	run, err := Start(context.Background(), r, h.options(1))
	if err != nil {
		t.Fatal(err)
	}
	if end := wait(t, run); end.Kind != RunFailed || !strings.Contains(end.Err, "no CLI runner") {
		t.Fatalf("end %+v", end)
	}
}

// CS-2: Claude works, Codex reviews; the review's VERDICT drives the
// until-done loop (ParseVerdict), the fix step gets the report.
func TestCLIVerifyLoop(t *testing.T) {
	h := newHarness(nil)
	f := newFakeExec(map[string][]execReply{
		"work":   {{text: "changed a.go"}},
		"review": {{text: "a.go:3 still nil\nVERDICT: FAIL"}, {text: "looks right\nVERDICT: PASS"}},
		"fix":    {{text: "made the map"}},
	})
	r := agentdag.Run{ID: "cl", Edges: agentdag.Chain("work", "review"),
		Nodes: []agentdag.Node{{ID: "work", Title: "Work", CLI: ai.CLIClaude}, {ID: "review", Title: "Review", CLI: ai.CLICodex}},
		Loop:  &agentdag.Loop{Verify: "review", MaxRounds: 3, Fix: agentdag.Node{ID: "fix", Title: "Fix", CLI: ai.CLIClaude}}}
	run, err := Start(context.Background(), r, cliOptions(h, f))
	if err != nil {
		t.Fatal(err)
	}
	end := wait(t, run)
	if end.Kind != RunDone || end.Verdict != VerdictPass || end.Round != 2 {
		t.Fatalf("end %+v", end)
	}
	if in := f.inputs("fix"); len(in) != 1 || !strings.Contains(in[0], "### Verify report (round 1)\na.go:3 still nil") {
		t.Errorf("fix input %q", in)
	}
	if in := f.inputs("review"); len(in) != 2 || !strings.Contains(in[1], "made the map") {
		t.Errorf("review inputs %q", in)
	}
	if d, _ := h.find(NodeDone, "review"); d.Verdict == "" {
		t.Error("the review's NodeDone has no verdict")
	}
}

// A CLI verify answer without a verdict line is asked once more (the
// repair prompt goes in the CLI's input with its first answer).
func TestCLIVerifyRepair(t *testing.T) {
	h := newHarness(nil)
	f := newFakeExec(map[string][]execReply{"review": {{text: "seems fine"}, {text: "VERDICT: PASS"}}})
	r := agentdag.Run{ID: "cl", Nodes: []agentdag.Node{{ID: "review", Title: "Review", CLI: ai.CLICodex}},
		Loop: &agentdag.Loop{Verify: "review", MaxRounds: 1, Fix: agentdag.Node{ID: "fix", CLI: ai.CLIClaude}}}
	run, err := Start(context.Background(), r, cliOptions(h, f))
	if err != nil {
		t.Fatal(err)
	}
	if end := wait(t, run); end.Kind != RunDone || end.Verdict != VerdictPass {
		t.Fatalf("end %+v", end)
	}
	in := f.inputs("review")
	if len(in) != 2 || !strings.Contains(in[1], VerifyRepairPrompt) || !strings.Contains(in[1], "seems fine") {
		t.Errorf("inputs %q", in)
	}
	if _, ok := h.find(NodeRepair, "review"); !ok {
		t.Error("no repair event")
	}
}

// CS-4: a usage limit pauses the run at that node (it goes back to
// pending, RunPaused says the limit and the reset time); Continue runs
// the node again and the run goes on.
func TestCLILimitPausesThenContinues(t *testing.T) {
	h := newHarness(map[string]*fakeProv{"b": {replies: []ai.Chunk{{Delta: "b done"}}}})
	reset := time.Date(2026, 10, 27, 16, 0, 0, 0, time.UTC)
	f := newFakeExec(map[string][]execReply{"a": {
		{err: &ai.LimitError{CLI: ai.CLIClaude, Msg: "You've hit your limit · resets 4pm", ResetAt: reset}},
		{text: "done after the reset"},
	}})
	r := chain("a", "b")
	r.Nodes[0].CLI = ai.CLIClaude
	run, err := Start(context.Background(), r, cliOptions(h, f))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pause", func() bool { _, ok := h.find(RunPaused, ""); return ok })
	p, _ := h.find(RunPaused, "")
	if p.Limit == "" || !p.ResetAt.Equal(reset) || p.Budget != 0 {
		t.Errorf("paused %+v", p)
	}
	if l, ok := h.find(NodeLimited, "a"); !ok || !strings.Contains(l.Err, "hit your limit") || !l.ResetAt.Equal(reset) {
		t.Errorf("limited %+v", l)
	}
	if got := states(run.Snapshot()); got != "a=pending b=pending" {
		t.Errorf("states while paused %s", got)
	}
	if _, failed := h.find(NodeFailed, "a"); failed {
		t.Error("a limit is not a failure")
	}
	run.Continue()
	end := wait(t, run)
	if end.Kind != RunDone || end.Output != "b done" {
		t.Fatalf("end %+v", end)
	}
	if n := len(f.inputs("a")); n != 2 {
		t.Errorf("a ran %d times", n)
	}
	if !strings.Contains(h.input("b"), "done after the reset") {
		t.Errorf("b input %q", h.input("b"))
	}
}

// Stop while paused by a limit cancels the run.
func TestCLILimitThenStop(t *testing.T) {
	h := newHarness(nil)
	f := newFakeExec(map[string][]execReply{"a": {{err: &ai.LimitError{CLI: ai.CLICodex, Msg: "usage limit"}}}})
	r := chain("a")
	r.Nodes[0].CLI = ai.CLICodex
	run, err := Start(context.Background(), r, cliOptions(h, f))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pause", func() bool { _, ok := h.find(RunPaused, ""); return ok })
	run.Cancel()
	if end := wait(t, run); end.Kind != RunCancelled {
		t.Fatalf("end %+v", end)
	}
	if got := states(run.Snapshot()); got != "a=cancelled" {
		t.Errorf("states %s", got)
	}
}
