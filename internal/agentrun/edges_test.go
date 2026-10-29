package agentrun

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// A bug in a step's builder (a panic) fails that step; the IDE and the
// rest of the run go on.
func TestStepPanicFailsTheStep(t *testing.T) {
	h := newHarness(nil)
	o := h.options(1)
	o.Build = func(context.Context, agentdag.Node, string) (*ai.Agent, []ai.Message, error) {
		var m map[string]int
		m["x"] = 1 // a nil-map write
		return nil, nil, nil
	}
	run, err := Start(context.Background(), chain("a"), o)
	if err != nil {
		t.Fatal(err)
	}
	if end := wait(t, run); end.Kind != RunFailed || !strings.Contains(end.Err, "step crashed") {
		t.Fatalf("end %+v", end)
	}
}

func TestBuildWithoutAgentFails(t *testing.T) {
	h := newHarness(nil)
	o := h.options(1)
	o.Build = func(context.Context, agentdag.Node, string) (*ai.Agent, []ai.Message, error) { return nil, nil, nil }
	run, err := Start(context.Background(), chain("a"), o)
	if err != nil {
		t.Fatal(err)
	}
	if end := wait(t, run); end.Kind != RunFailed || !strings.Contains(end.Err, "no agent") {
		t.Fatalf("end %+v", end)
	}
}

// A run started with no Emit hook (a headless run) still ends.
func TestRunWithoutEmit(t *testing.T) {
	h := newHarness(map[string]*fakeProv{"a": {}})
	o := h.options(1)
	o.Emit = nil
	run, err := Start(context.Background(), chain("a"), o)
	if err != nil {
		t.Fatal(err)
	}
	if end := wait(t, run); end.Kind != RunDone {
		t.Fatalf("end %+v", end)
	}
}

func TestFinalKinds(t *testing.T) {
	for _, k := range []EventKind{RunDone, RunFailed, RunCancelled} {
		if !k.Final() {
			t.Errorf("%v is final", k)
		}
	}
	for _, k := range []EventKind{NodeText, RunAwaitApproval, RunPaused} {
		if k.Final() {
			t.Errorf("%v is not final", k)
		}
	}
}

// Tools see whether their run has steps that run at the same time (the
// review tools then do not ask in parallel).
func TestParallelInToolContext(t *testing.T) {
	for _, parallel := range []int{1, 2} {
		call := ai.ToolCall{ID: "c1", Name: "edit"}
		h := newHarness(map[string]*fakeProv{"a": {replies: []ai.Chunk{{ToolCalls: []ai.ToolCall{call}}, {Delta: "done"}}}})
		var got bool
		h.tools = []ai.Tool{{Name: "edit", Run: func(ctx context.Context, _ map[string]any) (string, error) {
			got = Parallel(ctx)
			return "ok", nil
		}}}
		run, err := Start(context.Background(), chain("a"), h.options(parallel))
		if err != nil {
			t.Fatal(err)
		}
		wait(t, run)
		if got != (parallel > 1) {
			t.Errorf("parallel %d: tool saw %v", parallel, got)
		}
	}
	if Parallel(context.Background()) {
		t.Error("a plain context is not parallel")
	}
}

// A long streamed answer with wide runes keeps a short, valid tail.
func TestNodeTailOfLongWideText(t *testing.T) {
	long := strings.Repeat("é", tailWindow) // 2 bytes each: more than the window
	h := newHarness(map[string]*fakeProv{"a": {replies: []ai.Chunk{{Delta: "x" + long + "end"}}}})
	run, err := Start(context.Background(), chain("a"), h.options(1))
	if err != nil {
		t.Fatal(err)
	}
	wait(t, run)
	ev, ok := h.find(NodeText, "a")
	if !ok {
		t.Fatal("no text event")
	}
	if !utf8.ValidString(ev.Tail) || utf8.RuneCountInString(ev.Tail) != TailLen || !strings.HasSuffix(ev.Tail, "end") {
		t.Errorf("tail %d runes, valid %v", utf8.RuneCountInString(ev.Tail), utf8.ValidString(ev.Tail))
	}
}

func TestCLIStepErrors(t *testing.T) {
	boom := errors.New("codex crashed")
	cases := map[string]struct {
		run     agentdag.Run
		replies map[string][]execReply
		check   func(*harness, Event) bool
	}{
		"exec error": {
			run:     func() agentdag.Run { r := chain("a"); r.Nodes[0].CLI = ai.CLICodex; return r }(),
			replies: map[string][]execReply{"a": {{err: boom}}},
			check:   func(_ *harness, e Event) bool { return e.Kind == RunFailed && strings.Contains(e.Err, "codex crashed") },
		},
		"repair fails": {
			run: agentdag.Run{ID: "cl", Nodes: []agentdag.Node{{ID: "review", Title: "Review", CLI: ai.CLICodex}},
				Loop: &agentdag.Loop{Verify: "review", MaxRounds: 1, Fix: agentdag.Node{ID: "fix", CLI: ai.CLIClaude}}},
			replies: map[string][]execReply{"review": {{text: "seems fine"}, {err: boom}}},
			check:   func(_ *harness, e Event) bool { return e.Kind == RunFailed && strings.Contains(e.Err, "codex crashed") },
		},
		"no verdict twice": {
			run: agentdag.Run{ID: "cl", Nodes: []agentdag.Node{{ID: "review", Title: "Review", CLI: ai.CLICodex}},
				Loop: &agentdag.Loop{Verify: "review", MaxRounds: 1, Fix: agentdag.Node{ID: "fix", CLI: ai.CLIClaude}}},
			replies: map[string][]execReply{"review": {{text: "seems fine"}, {text: "still fine"}}},
			check: func(h *harness, e Event) bool {
				d, _ := h.find(NodeDone, "review")
				return e.Verdict == VerdictFail && d.Verdict == VerdictMissing
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(nil)
			run, err := Start(context.Background(), c.run, cliOptions(h, newFakeExec(c.replies)))
			if err != nil {
				t.Fatal(err)
			}
			if end := wait(t, run); !c.check(h, end) {
				t.Fatalf("end %+v", end)
			}
		})
	}
}

func TestApplyPlanEditRefusesBadSteps(t *testing.T) {
	long := strings.Repeat("x", PlanPromptMaxBytes+1)
	for text, want := range map[string]string{
		`{"nodes": [{"id": "bad id"}]}`:                                  "use letters",
		`{"nodes": [{"id": "read"}, {"id": "read"}]}`:                    `duplicate step id "read"`,
		`{"nodes": [{"id": "read", "prompt": "` + long + `"}]}`:          "longer than 8 KB",
		`{"nodes": [{"id": "read", "category": "fast", "prompt": "x"}]}`: `unknown category "fast"`,
	} {
		if _, err := ApplyPlanEdit(reviewRun(), text, nil); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%.60s: err %v, want %q", text, err, want)
		}
	}
}

// A new step without title or category gets its id and the quick slot;
// a planned step moved to deep moves to the deep slot.
func TestApplyPlanEditDefaultsAndCategoryMove(t *testing.T) {
	cats := map[string]string{agentdag.CategoryQuick: "cheap", agentdag.CategoryDeep: "strong"}
	r := reviewRun()
	r.Nodes[1].Prompt, r.Nodes[1].Category, r.Nodes[1].Slot = "Write it.", agentdag.CategoryQuick, "cheap"
	out, err := ApplyPlanEdit(r, `{"nodes": [
		{"id": "read", "deps": []},
		{"id": "write", "prompt": "Write it well.", "category": "deep", "deps": ["read"]},
		{"id": "notes", "prompt": "Write notes.", "deps": ["write"]}
	]}`, cats)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]agentdag.Node{}
	for _, n := range out.Nodes {
		byID[n.ID] = n
	}
	if w := byID["write"]; w.Category != agentdag.CategoryDeep || w.Slot != "strong" || w.Prompt != "Write it well." {
		t.Errorf("write %+v", w)
	}
	if n := byID["notes"]; n.Title != "notes" || n.Category != agentdag.CategoryQuick || n.Slot != "cheap" {
		t.Errorf("notes %+v", n)
	}
}

func TestParsePlanRefusesLongPrompt(t *testing.T) {
	long := strings.Repeat("y", PlanPromptMaxBytes+1)
	_, _, err := ParsePlan(`{"nodes": [{"id": "a", "title": "A", "prompt": "`+long+`"}]}`, nil)
	if err == nil || !strings.Contains(err.Error(), "longer than 8 KB") {
		t.Errorf("err %v", err)
	}
}
