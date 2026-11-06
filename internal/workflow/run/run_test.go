package run

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// flow is a small source → filter → each(ai, write) → write workflow.
const flow = `{"version": 1, "name": "w", "vars": {"label": "bug", "base": "main"},
 "steps": [
  {"id": "bugs", "type": "source", "from": "github", "what": "issues", "args": {"label": "{{vars.label}}"}},
  {"id": "ready", "type": "filter", "when": "item.ready"},
  {"id": "fix", "type": "each", "items": "steps.ready.items", "worktree": true, "base": "{{vars.base}}", "steps": [
    {"id": "code", "type": "ai", "model": "quick", "prompt": "fix {{item.title}}"},
    {"id": "pr", "type": "write", "to": "pr", "title": "fix: {{item.title}}", "body": "{{steps.code.text}}"}
  ]},
  {"id": "sum", "type": "write", "to": "message", "via": "os", "text": "{{len(steps.fix.done)}} done"}
 ]}`

func TestRunRealAndDry(t *testing.T) {
	cases := []struct {
		name   string
		dry    bool
		calls  []string
		events []string
		writes []string // to:state:title-or-text
	}{
		{
			name: "real",
			calls: []string{
				"source bugs args=map[label:bug]",
				"enter a base=main",
				`run code/a prompt="fix T a" wt=/wt/a`,
				`write pr/a to=pr title="fix: T a" body="out of code/a" marker=termocode-run:r1/pr/a resumed=false`,
				"leave a ok=true",
				"enter c base=main",
				`run code/c prompt="fix T c" wt=/wt/c`,
				`write pr/c to=pr title="fix: T c" body="out of code/c" marker=termocode-run:r1/pr/c resumed=false`,
				"leave c ok=true",
				`write sum to=message title="" body="" marker=termocode-run:r1/sum resumed=false`,
			},
			events: []string{
				"step_start bugs", "step_done bugs", "step_start ready", "step_done ready", "step_start fix",
				"item_start fix a", "step_start code a", "step_done code a",
				"step_start pr a", "write_wait pr a", "write_allowed pr a", "step_done pr a", "item_done fix a",
				"item_start fix c", "step_start code c", "step_done code c",
				"step_start pr c", "write_wait pr c", "write_allowed pr c", "step_done pr c", "item_done fix c",
				"step_done fix", "step_start sum", "write_wait sum", "write_allowed sum", "step_done sum", "run_done",
			},
			writes: []string{"pr:done:fix: T a", "pr:done:fix: T c", "message:done:2 done"},
		},
		{
			name:  "dry",
			dry:   true,
			calls: []string{"source bugs args=map[label:bug]"},
			events: []string{
				"step_start bugs", "step_done bugs", "step_start ready", "step_done ready", "step_start fix",
				"item_start fix a", "step_start code a", "step_skipped code a", "step_start pr a", "step_done pr a", "item_done fix a",
				"item_start fix c", "step_start code c", "step_skipped code c", "step_start pr c", "step_done pr c", "item_done fix c",
				"step_done fix", "step_start sum", "step_done sum", "run_done",
			},
			writes: []string{"pr:recorded:fix: T a", "pr:recorded:fix: T c", "message:recorded:2 done"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fk := newFakes(items("a", "b", "c")...)
			var ev events
			opts := fk.options()
			opts.Dry = tc.dry
			opts.OnEvent = ev.on
			rf := wait(t, Start(context.Background(), mustParse(t, flow), opts))
			if rf.State != RunOK || rf.Dry != tc.dry {
				t.Fatalf("state %q dry %v err %q", rf.State, rf.Dry, rf.Error)
			}
			if got := fk.all(); !slices.Equal(got, tc.calls) {
				t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.calls, "\n"))
			}
			if got := ev.kinds(); !slices.Equal(got, tc.events) {
				t.Errorf("events:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.events, "\n"))
			}
			var writes []string
			for _, w := range rf.Writes {
				writes = append(writes, w.To+":"+w.State+":"+w.Spec.Title+w.Spec.Text)
			}
			if !slices.Equal(writes, tc.writes) {
				t.Errorf("writes %q, want %q", writes, tc.writes)
			}
			fix := rf.Steps["fix"].Each
			if len(fix.Items) != 2 || fix.Items[0].State != StateDone || fix.Items[1].State != StateDone {
				t.Fatalf("fix items %+v", fix.Items)
			}
			wantCode := StateDone
			if tc.dry {
				wantCode = StateWouldRun
			}
			if st := fix.Items[0].Steps["code"].State; st != wantCode {
				t.Errorf("code state %q, want %q", st, wantCode)
			}
		})
	}
}

func TestWhenFalseSkipsAndRunsElse(t *testing.T) {
	src := `{"version": 1, "name": "w", "vars": {"go": false}, "steps": [
	  {"id": "t", "type": "tests", "task": "test", "when": "vars.go",
	   "else": [{"id": "no", "type": "shell", "argv": ["echo"]}]},
	  {"id": "yes", "type": "shell", "argv": ["echo"], "when": "not vars.go",
	   "else": [{"id": "never", "type": "shell", "argv": ["echo"]}]}
	]}`
	fk := newFakes()
	rf := wait(t, Start(context.Background(), mustParse(t, src), fk.options()))
	if rf.State != RunOK {
		t.Fatalf("state %q %q", rf.State, rf.Error)
	}
	want := []string{`run no prompt="" wt=`, `run yes prompt="" wt=`}
	if got := fk.all(); !slices.Equal(got, want) {
		t.Fatalf("calls %q, want %q", got, want)
	}
	if rf.Steps["t"].State != StateSkipped || rf.Steps["no"].State != StateDone {
		t.Fatalf("states t=%q no=%q", rf.Steps["t"].State, rf.Steps["no"].State)
	}
}

// itemFlow fails code for item a; else comments; on_error is set per case.
const itemFlow = `{"version": 1, "name": "w", "on_error": "%s", "steps": [
  {"id": "bugs", "type": "source", "from": "github", "what": "issues"},
  {"id": "fix", "type": "each", "items": "steps.bugs.items", "steps": [
    {"id": "code", "type": "tests", "task": "test",
     "else": [{"id": "sorry", "type": "write", "to": "github.comment", "pr": "1", "body": "failed {{item.id}}"}]},
    {"id": "pr", "type": "write", "to": "pr", "title": "fix {{item.id}}"}
  ]},
  {"id": "sum", "type": "write", "to": "message", "via": "os", "text": "{{len(steps.fix.failed)}} failed"}
]}`

func TestOnErrorContinueRunsElseThenNextItem(t *testing.T) {
	fk := newFakes(items("a", "c")...)
	fk.fail["code/a"] = []error{errors.New("tests red")}
	rf := wait(t, Start(context.Background(), mustParse(t, strings.Replace(itemFlow, "%s", "continue", 1)), fk.options()))
	if rf.State != RunOK {
		t.Fatalf("state %q %q", rf.State, rf.Error)
	}
	var got []string
	for _, c := range fk.all() {
		if strings.HasPrefix(c, "write") {
			got = append(got, strings.Fields(c)[1])
		}
	}
	if want := []string{"sorry/a", "pr/c", "sum"}; !slices.Equal(got, want) {
		t.Fatalf("writes %q, want %q", got, want)
	}
	a := rf.Steps["fix"].Each.Items[0]
	if a.State != StateFailed || a.Steps["code"].State != StateFailed || a.Steps["code"].Error != "tests red" {
		t.Fatalf("item a %+v code %+v", a, a.Steps["code"])
	}
	if _, ran := a.Steps["pr"]; ran {
		t.Fatal("pr ran for the failed item")
	}
	if text := rf.Writes[len(rf.Writes)-1].Spec.Text; text != "1 failed" {
		t.Fatalf("sum text %q", text)
	}
}

func TestOnErrorStopEndsTheRun(t *testing.T) {
	fk := newFakes(items("a", "c")...)
	fk.fail["code/a"] = []error{errors.New("tests red")}
	var ev events
	opts := fk.options()
	opts.OnEvent = ev.on
	rf := wait(t, Start(context.Background(), mustParse(t, strings.Replace(itemFlow, "%s", "stop", 1)), opts))
	if rf.State != RunFail || !strings.Contains(rf.Error, "tests red") {
		t.Fatalf("state %q %q", rf.State, rf.Error)
	}
	its := rf.Steps["fix"].Each.Items
	if its[0].State != StateFailed || its[1].State != StatePending {
		t.Fatalf("items %q %q", its[0].State, its[1].State)
	}
	if len(rf.Writes) != 0 {
		t.Fatalf("writes %+v (else must not run on stop)", rf.Writes)
	}
	if k := ev.kinds(); k[len(k)-1] != "run_failed" {
		t.Fatalf("last event %q", k[len(k)-1])
	}
}

func TestWriteDeniedFailsTheItemOnly(t *testing.T) {
	fk := newFakes(items("a", "c")...)
	fk.deny["pr"] = true
	var ev events
	opts := fk.options()
	opts.OnEvent = ev.on
	rf := wait(t, Start(context.Background(), mustParse(t, strings.Replace(itemFlow, "%s", "continue", 1)), opts))
	if rf.State != RunOK {
		t.Fatalf("state %q %q", rf.State, rf.Error)
	}
	for _, it := range rf.Steps["fix"].Each.Items {
		if it.State != StateFailed || it.Steps["pr"].State != StateDenied {
			t.Fatalf("item %s %q pr %q", it.Key, it.State, it.Steps["pr"].State)
		}
	}
	if !slices.Contains(ev.kinds(), "write_denied pr a") {
		t.Fatalf("events %q", ev.kinds())
	}
	if fk.tried("pr/a") != 0 {
		t.Fatal("a denied write was tried")
	}
}

func TestPlanMarksItems(t *testing.T) {
	src := `{"version": 1, "name": "w", "steps": [
	  {"id": "bugs", "type": "source", "from": "github", "what": "issues"},
	  {"id": "order", "type": "plan", "model": "quick"},
	  {"id": "ready", "type": "filter", "when": "not item.plan.skip", "sort": "item.plan.rank"}
	]}`
	for _, noPlan := range []bool{false, true} {
		fk := newFakes(items("a", "b", "c")...)
		fk.plan = PlanResult{Order: []string{"c", "a", "b"}, Skip: map[string]string{"b": "dup"}}
		opts := fk.options()
		opts.NoPlan = noPlan
		rf := wait(t, Start(context.Background(), mustParse(t, src), opts))
		var keys []string
		for _, it := range rf.Steps["ready"].Items {
			keys = append(keys, it.Key)
		}
		want, wantState := []string{"c", "a"}, StateDone
		if noPlan {
			want, wantState = []string{"a", "b", "c"}, StateSkipped
		}
		if !slices.Equal(keys, want) || rf.Steps["order"].State != wantState {
			t.Fatalf("noPlan=%v: kept %q (want %q), plan state %q", noPlan, keys, want, rf.Steps["order"].State)
		}
	}
}

func TestCancelMidItem(t *testing.T) {
	fk := newFakes(items("a", "c")...)
	fk.block = "code/a"
	var ev events
	opts := fk.options()
	opts.OnEvent = ev.on
	h := Start(context.Background(), mustParse(t, strings.Replace(flow, `"when": "item.ready"`, `"when": "true"`, 1)), opts)
	<-fk.blocked
	h.Cancel()
	rf := wait(t, h)
	if rf.State != RunCancel {
		t.Fatalf("state %q %q", rf.State, rf.Error)
	}
	its := rf.Steps["fix"].Each.Items
	if its[0].State != StateCancelled || its[0].Steps["code"].State != StateCancelled || its[1].State != StatePending {
		t.Fatalf("items %q (code %q) %q", its[0].State, its[0].Steps["code"].State, its[1].State)
	}
	if _, ok := rf.Steps["sum"]; ok || len(rf.Writes) != 0 {
		t.Fatal("steps ran after cancel")
	}
	if !slices.Contains(fk.all(), "leave a ok=false") {
		t.Fatalf("worktree not left: %q", fk.all())
	}
	if k := ev.kinds(); k[len(k)-1] != "run_cancelled" {
		t.Fatalf("last event %q", k[len(k)-1])
	}
	h.Cancel() // again: no panic
}

// Spec (WFX-18): a message's channel is a template, filled like its text.
func TestMessageChannelIsRendered(t *testing.T) {
	src := `{"version": 1, "name": "w", "vars": {"ch": "C0123"}, "steps": [
	  {"id": "sum", "type": "write", "to": "message", "via": "slack", "channel": "{{vars.ch}}", "text": "hi"}]}`
	fk := newFakes()
	rf := wait(t, Start(context.Background(), mustParse(t, src), fk.options()))
	if rf.State != RunOK || len(rf.Writes) != 1 || rf.Writes[0].Spec.Channel != "C0123" {
		t.Fatalf("state %q writes %+v", rf.State, rf.Writes)
	}
}
