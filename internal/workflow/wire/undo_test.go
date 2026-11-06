package wire

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/run"
)

type undoTools struct{ calls []string }

func (u *undoTools) CallTool(_ context.Context, server, tool string, args map[string]any) (json.RawMessage, bool, error) {
	b, _ := json.Marshal(args)
	u.calls = append(u.calls, server+" "+tool+" "+string(b))
	return json.RawMessage(`{}`), false, nil
}

// Spec (WFX-20, plan §5.5, §6.6): the write ledger keeps each write's
// undo recipe; Undo runs it once: an MCP tool call on the write's
// server, gh pr close for a pull request, git reset --soft in the item's
// worktree for a commit. A write without a recipe, one undone already or
// one older than 7 days is refused.
func TestUndoRunsTheRecipe(t *testing.T) {
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	t.Cleanup(clock.Set(func() time.Time { return now }))
	root, ws := t.TempDir(), t.TempDir()
	l, err := ledger.Open(root, "wf", "r1")
	if err != nil {
		t.Fatal(err)
	}
	ok := func(context.Context, string) (ledger.Result, error) { return ledger.Result{ExternalID: "x"}, nil }
	for _, w := range []ledger.Write{
		{Step: "state", Item: "LIN-1", Target: "linear · update_issue", Undo: &ledger.Undo{Title: "state → Todo", Tool: "update_issue", Args: map[string]string{"state": "Todo"}}},
		{Step: "pr", Item: "LIN-2", Target: "pr", Undo: &ledger.Undo{Title: "close the PR", Tool: "github.pr.close", Args: map[string]string{"branch": "wf/lin-2", "delete_branch": "true"}}},
		{Step: "commit", Item: "LIN-3", Target: "commit", Undo: &ledger.Undo{Title: "drop the commit", Tool: "git.reset", Args: map[string]string{"branch": "wf/lin-3", "to": "HEAD~1", "mode": "soft"}}},
		{Step: "note", Item: "LIN-1", Target: "http"},
	} {
		if _, _, err := l.Do(context.Background(), w, ok, nil); err != nil {
			t.Fatal(err)
		}
	}
	rf := &run.RunFile{ID: "r1", Workflow: "wf", Writes: []run.WriteRecord{
		{Step: "state", Item: "LIN-1", To: "mcp", State: "done", Spec: workflow.WriteSpec{To: "mcp", Server: "linear"}},
	}, Steps: map[string]*run.StepState{"fix": {State: run.StateDone, Each: &run.EachState{Items: []*run.ItemState{
		{Key: "LIN-3", State: run.StateDone, Env: run.ItemEnv{Worktree: "/wt/lin-3", Branch: "wf/lin-3"}},
	}}}}}
	tools := &undoTools{}
	var ran []string
	u := Undoer{Root: root, Workspace: ws, Tools: tools, Exec: func(_ context.Context, argv []string, dir string) (string, int, error) {
		ran = append(ran, dir+": "+strings.Join(argv, " "))
		return "", 0, nil
	}}
	list := Undoable(root, rf)
	if len(list) != 3 || list[0].Title == "" {
		t.Fatalf("undoable %+v", list)
	}
	for _, it := range []struct{ step, item string }{{"state", "LIN-1"}, {"pr", "LIN-2"}, {"commit", "LIN-3"}} {
		if err := u.Undo(context.Background(), rf, it.step, it.item); err != nil {
			t.Fatalf("undo %s: %v", it.step, err)
		}
	}
	if len(tools.calls) != 1 || tools.calls[0] != `linear update_issue {"state":"Todo"}` {
		t.Errorf("tool calls %q", tools.calls)
	}
	if len(ran) != 2 || ran[0] != ws+": gh pr close wf/lin-2 --delete-branch" || ran[1] != "/wt/lin-3: git reset --soft HEAD~1" {
		t.Errorf("commands %q", ran)
	}
	if err := u.Undo(context.Background(), rf, "state", "LIN-1"); err == nil || !strings.Contains(err.Error(), "undone already") {
		t.Errorf("second undo: %v", err)
	}
	if err := u.Undo(context.Background(), rf, "note", "LIN-1"); err == nil || !strings.Contains(err.Error(), "cannot be undone") {
		t.Errorf("no recipe: %v", err)
	}
	for _, it := range Undoable(root, rf) {
		if !it.Undone {
			t.Errorf("%s not marked undone", it.Step)
		}
	}
	now = now.Add(8 * 24 * time.Hour)
	l2, _ := ledger.Open(root, "wf", "r2")
	if _, _, err := l2.Do(context.Background(), ledger.Write{Step: "s", Undo: &ledger.Undo{Title: "t", Tool: "x"}}, ok, nil); err != nil {
		t.Fatal(err)
	}
	now = now.Add(8 * 24 * time.Hour)
	if err := u.Undo(context.Background(), &run.RunFile{ID: "r2", Workflow: "wf"}, "s", ""); err == nil || !strings.Contains(err.Error(), "7 days") {
		t.Errorf("old write: %v", err)
	}
}

// Spec (WFX-20, plan §6.6): an MCP undo whose argument held a secret
// keeps "[REDACTED]" in the ledger, never the value. Undo refuses that
// write before it calls anything, names the argument and says to undo
// it by hand; it never sends a guessed value. Undoable marks it so the
// run page and the card can say so first.
func TestUndoRefusesARedactedArgument(t *testing.T) {
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	t.Cleanup(clock.Set(func() time.Time { return now }))
	root := t.TempDir()
	l, err := ledger.Open(root, "wf", "r1")
	if err != nil {
		t.Fatal(err)
	}
	ok := func(context.Context, string) (ledger.Result, error) { return ledger.Result{}, nil }
	tok := "sk-" + "test0123456789abcdefghij"
	if _, _, err := l.Do(context.Background(), ledger.Write{Step: "hook", Item: "LIN-1", Target: "slack · set_topic",
		Undo: &ledger.Undo{Title: "topic back", Tool: "set_topic", Args: map[string]string{"channel": "dev", "api_key": tok, "note": "Bearer " + tok}}}, ok, nil); err != nil {
		t.Fatal(err)
	}
	rf := &run.RunFile{ID: "r1", Workflow: "wf", Writes: []run.WriteRecord{
		{Step: "hook", Item: "LIN-1", To: "mcp", State: "done", Spec: workflow.WriteSpec{To: "mcp", Server: "slack"}}}}
	list := Undoable(root, rf)
	if len(list) != 1 || list[0].Blocked == "" || !strings.Contains(list[0].Blocked, "api_key") || !strings.Contains(list[0].Blocked, "note") {
		t.Fatalf("undoable %+v: want it marked blocked, naming api_key and note", list)
	}
	tools := &undoTools{}
	for _, u := range []Undoer{{Root: root, Tools: tools}, {Root: root}} { // with and without a connected server
		err := u.Undo(context.Background(), rf, "hook", "LIN-1")
		if err == nil || !strings.Contains(err.Error(), "api_key") || !strings.Contains(err.Error(), "by hand") || strings.Contains(err.Error(), tok) {
			t.Errorf("undo of a redacted argument: %v", err)
		}
	}
	if len(tools.calls) != 0 {
		t.Fatalf("a tool was called with a guessed value: %q", tools.calls)
	}
	if got := Undoable(root, rf); got[0].Undone {
		t.Error("a refused undo was marked undone")
	}
}
