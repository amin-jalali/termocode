package workflow

import (
	"strings"
	"testing"
	"time"
)

// CMP-42: the agent graph steps. A loop repeats its steps with a round
// cap (and a cost cap), a branch runs lists side by side and joins them,
// a wait step polls a pull request, and handoff passes one step's output
// to the next agent.

const graphFile = `{"version": 1, "name": "graph", "steps": [
  {"id": "plan", "type": "ai", "model": "quick", "prompt": "plan it"},
  {"id": "review", "type": "branch", "join": "any", "branches": [
    {"name": "security", "steps": [{"id": "sec", "type": "agent", "agent": "codex", "prompt": "check", "handoff": "plan"}]},
    {"name": "style", "steps": [{"id": "sty", "type": "ai", "model": "quick", "prompt": "style"}]}
  ]},
  {"id": "fix", "type": "loop", "max": 4, "until": "steps.t.ok", "budget_usd": 2.5, "steps": [
    {"id": "code", "type": "agent", "agent": "claude-code", "prompt": "fix round {{loop.round}}", "handoff": "sec"},
    {"id": "t", "type": "tests", "task": "test"}
  ]},
  {"id": "ci", "type": "wait", "for": "ci", "pr": "{{steps.open.url}}", "every": "30s", "timeout": "2h"},
  {"id": "open", "type": "write", "to": "pr", "title": "x"}
]}`

func TestParseGraphSteps(t *testing.T) {
	f, warns, err := Parse("", []byte(graphFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) > 0 {
		t.Fatalf("warnings %v", warns)
	}
	b := f.Steps[1].Branch
	if b == nil || b.Join != JoinAny || len(b.Branches) != 2 || b.Branches[0].Name != "security" || len(b.Branches[1].Steps) != 1 {
		t.Fatalf("branch %+v", b)
	}
	if h := b.Branches[0].Steps[0].Handoff; h != "plan" {
		t.Fatalf("handoff %q", h)
	}
	l := f.Steps[2].Loop
	if l == nil || l.Max != 4 || l.Until != "steps.t.ok" || l.BudgetUSD != 2.5 || len(l.Steps) != 2 {
		t.Fatalf("loop %+v", l)
	}
	w := f.Steps[3].Wait
	if w == nil || w.For != WaitCI || w.PR != "{{steps.open.url}}" || w.Every != 30*time.Second || f.Steps[3].Timeout != 2*time.Hour {
		t.Fatalf("wait %+v", w)
	}
	n := 0
	Walk(f.Steps, func(*Step) bool { n++; return true })
	if n != 9 {
		t.Fatalf("Walk saw %d steps, want 9 (nested loop and branch steps too)", n)
	}
	if f.StepByID("t") == nil || f.StepByID("sty") == nil {
		t.Fatal("StepByID does not find steps inside a loop or a branch")
	}
	if got := len(f.Steps[2].Lists()); got != 1 {
		t.Fatalf("loop Lists %d", got)
	}
	if got := len(f.Steps[1].Lists()); got != 2 {
		t.Fatalf("branch Lists %d", got)
	}
}

func TestParseGraphDefaults(t *testing.T) {
	src := `{"version": 1, "name": "g", "steps": [
	  {"type": "branch", "branches": [{"name": "a", "steps": [{"type": "tests"}]}, {"name": "b", "steps": [{"type": "tests"}]}]},
	  {"type": "wait", "for": "merged", "pr": "7"}]}`
	f, _, err := Parse("", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if f.Steps[0].Branch.Join != JoinAll {
		t.Errorf("join default %q", f.Steps[0].Branch.Join)
	}
	if f.Steps[1].Wait.Every != DefaultWaitEvery {
		t.Errorf("every default %v", f.Steps[1].Wait.Every)
	}
}

func TestParseGraphErrors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"loop no max", `{"version":1,"name":"bad","steps":[{"type":"loop","steps":[{"type":"tests"}]}]}`, `"max" is missing`},
		{"loop max cap", `{"version":1,"name":"bad","steps":[{"type":"loop","max":21,"steps":[{"type":"tests"}]}]}`, "max: must be a whole number from 1 to 20"},
		{"loop no steps", `{"version":1,"name":"bad","steps":[{"type":"loop","max":2}]}`, `"steps" is missing`},
		{"branch one", `{"version":1,"name":"bad","steps":[{"type":"branch","branches":[{"name":"a","steps":[{"type":"tests"}]}]}]}`, "branches: needs 2 to 8 branches"},
		{"branch dup", `{"version":1,"name":"bad","steps":[{"type":"branch","branches":[{"name":"a","steps":[{"type":"tests"}]},{"name":"a","steps":[{"type":"tests"}]}]}]}`, `"a" is already the name of a branch`},
		{"branch bad name", `{"version":1,"name":"bad","steps":[{"type":"branch","branches":[{"name":"A B","steps":[{"type":"tests"}]},{"name":"b","steps":[{"type":"tests"}]}]}]}`, "name: use a-z"},
		{"branch join", `{"version":1,"name":"bad","steps":[{"type":"branch","join":"first","branches":[{"name":"a","steps":[{"type":"tests"}]},{"name":"b","steps":[{"type":"tests"}]}]}]}`, `join: "first" is not allowed; use one of: all, any`},
		{"wait for", `{"version":1,"name":"bad","steps":[{"type":"wait","for":"deploy","pr":"1"}]}`, `for: "deploy" is not allowed; use one of: ci, merged, approved`},
		{"wait no pr", `{"version":1,"name":"bad","steps":[{"type":"wait","for":"ci"}]}`, `"pr" is missing`},
		{"wait every fast", `{"version":1,"name":"bad","steps":[{"type":"wait","for":"ci","pr":"1","every":"2s"}]}`, "every: must be at least 10s"},
		{"handoff unknown", `{"version":1,"name":"bad","steps":[{"type":"ai","prompt":"x","handoff":"nope"}]}`, `handoff: no step has the id "nope"`},
		{"handoff self", `{"version":1,"name":"bad","steps":[{"id":"a","type":"ai","prompt":"x","handoff":"a"}]}`, "handoff: a step cannot hand off to itself"},
		{"handoff on tests", `{"version":1,"name":"bad","steps":[{"id":"a","type":"ai","prompt":"x"},{"type":"tests","handoff":"a"}]}`, `"handoff" is only for ai, agent and pipeline steps`},
		{"nested deep", `{"version":1,"name":"bad","steps":[{"type":"loop","max":2,"steps":[{"type":"loop","max":2,"steps":[{"type":"loop","max":2,"steps":[{"type":"loop","max":2,"steps":[{"type":"tests"}]}]}]}]}]}`, "is nested 4 deep; the most is 3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Parse("bad.jsonc", []byte(tc.src))
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q, want it to hold %q", err.Error(), tc.want)
			}
		})
	}
}
