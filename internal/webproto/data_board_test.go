package webproto

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// boardNow is 2026-11-07 15:00 in Berlin; today began at 00:00 there.
func boardNow(t *testing.T) (time.Time, int64) {
	t.Helper()
	loc := time.FixedZone("CET", 3600)
	now := time.Date(2026, 11, 7, 15, 0, 0, 0, loc)
	return now, time.Date(2026, 11, 7, 0, 0, 0, 0, loc).UnixMilli()
}

func boardByID(b Board) map[string]BoardItem {
	out := map[string]BoardItem{}
	for _, it := range b.Items {
		out[it.ID] = it
	}
	return out
}

// Spec (MOB-18, plan §2.5, MOBUX-9): the board sorts what goes on into
// four columns. A card, a run that needs you, a paused run, a lane that
// waits and a workflow whose write waits are "needs you"; running runs,
// working lanes and running workflows are "working"; a finished lane is
// "review"; what ended today is "done" (✓ or ✗). Yesterday's runs and
// idle, asleep, main and external worktrees are not on the board.
func TestBuildBoardSortsIntoFourColumns(t *testing.T) {
	now, today := boardNow(t)
	hour := int64(time.Hour / time.Millisecond)
	in := BoardInput{
		Now:   now,
		Cards: CardsSnap{Cards: []Card{{ID: "c1", Kind: "tool", Title: "AI wants to: run go test", Since: today + 10*hour}}},
		Runs: RunsSnap{Runs: []RunItem{
			{Key: "r-run", Title: "rate limit", Profile: "default", State: "running", Step: 3, Steps: 5, Started: today + 14*hour, CostUSD: 0.8},
			{Key: "r-ask", Title: "fix login", State: "running", NeedsYou: true, Why: "waits for a card", Started: today + 13*hour},
			{Key: "r-pause", Title: "docs", State: "paused", Started: today + 12*hour},
			{Key: "r-done", Title: "lint", State: "done", Started: today + 9*hour, CostUSD: 0.4},
			{Key: "r-fail", Title: "e2e", State: "failed", Started: today + 8*hour},
			{Key: "r-old", Title: "yesterday", State: "done", Started: today - hour, CostUSD: 9},
		}},
		Worktrees: WorktreesSnap{State: "ready", Worktrees: []WorktreeItem{
			{ID: "main", Title: "main", Kind: "main", Group: "active", State: "working"},
			{ID: "w-wait", Title: "fix-login", Kind: "agent", Group: "active", State: "waiting", Agent: "claude"},
			{ID: "w-work", Title: "api", Kind: "agent", Group: "pinned", State: "working", Branch: "feat/api"},
			{ID: "w-done", Title: "docs", Kind: "user", Group: "active", State: "done", Ahead: 3, Dirty: 1},
			{ID: "w-idle", Title: "old", Kind: "user", Group: "active", State: "idle"},
			{ID: "w-sleep", Title: "zz", Kind: "agent", Group: "asleep", State: "asleep"},
			{ID: "w-ext", Title: "x", Kind: "external", Group: "external", State: "working"},
		}},
		Workflows: WorkflowsSnap{Runs: []WorkflowRun{
			{ID: "wf-gate", Workflow: "nightly", State: "running", Waiting: "write", Started: today + 11*hour},
			{ID: "wf-run", Workflow: "triage", State: "running", Started: today + 14*hour},
			{ID: "wf-done", Workflow: "report", State: "done", Started: today + 7*hour, Ended: today + 8*hour, Dry: true},
			{ID: "wf-old", Workflow: "report", State: "done", Started: today - 3*hour, Ended: today - 2*hour},
		}},
	}
	b := BuildBoard(in)
	want := map[string][2]string{
		"card:c1":          {BoardNeedsYou, BoardWait},
		"run:r-ask":        {BoardNeedsYou, BoardWait},
		"run:r-pause":      {BoardNeedsYou, BoardWait},
		"lane:w-wait":      {BoardNeedsYou, BoardWait},
		"workflow:wf-gate": {BoardNeedsYou, BoardWait},
		"run:r-run":        {BoardWorking, BoardRun},
		"lane:w-work":      {BoardWorking, BoardRun},
		"workflow:wf-run":  {BoardWorking, BoardRun},
		"lane:w-done":      {BoardReview, BoardOK},
		"run:r-done":       {BoardDone, BoardOK},
		"run:r-fail":       {BoardDone, BoardFail},
		"workflow:wf-done": {BoardDone, BoardOK},
	}
	got := boardByID(b)
	if len(got) != len(want) {
		var ids []string
		for _, it := range b.Items {
			ids = append(ids, it.ID)
		}
		t.Fatalf("items %v, want %d", ids, len(want))
	}
	for id, w := range want {
		it, ok := got[id]
		if !ok {
			t.Errorf("%s is not on the board", id)
			continue
		}
		if it.Column != w[0] || it.State != w[1] {
			t.Errorf("%s: %s/%s, want %s/%s", id, it.Column, it.State, w[0], w[1])
		}
	}
	if c := b.Counts; c.NeedsYou != 5 || c.Working != 3 || c.Review != 1 || c.Done != 3 {
		t.Errorf("counts %+v", c)
	}
	if b.TodayUSD != 1.2 {
		t.Errorf("today $%v, want 1.2 (yesterday's run is not today)", b.TodayUSD)
	}
	if it := got["run:r-run"]; it.Step != 3 || it.Steps != 5 || it.Sub != "default" || it.CostUSD != 0.8 || it.Ref != "r-run" || it.Kind != BoardKindRun {
		t.Errorf("run tile %+v", it)
	}
	if it := got["lane:w-done"]; it.Ahead != 3 || it.Dirty != 1 || it.Ref != "w-done" || it.Kind != BoardKindLane {
		t.Errorf("lane tile %+v", it)
	}
	if it := got["lane:w-work"]; it.Sub != "feat/api" {
		t.Errorf("a lane without an agent shows its branch: %+v", it)
	}
	if it := got["workflow:wf-gate"]; it.Why != "waits: write" || it.Ref != "wf-gate" {
		t.Errorf("workflow gate tile %+v", it)
	}
	if b.Built != now.UnixMilli() {
		t.Errorf("built %d", b.Built)
	}
}

// Spec (MOB-18): the columns come in order (needs you, working, review,
// done); needs you lists the oldest first (it waited longest), the
// others the newest first.
func TestBuildBoardOrder(t *testing.T) {
	now, today := boardNow(t)
	in := BoardInput{Now: now, Runs: RunsSnap{Runs: []RunItem{
		{Key: "d1", State: "done", Started: today + 1},
		{Key: "w1", State: "running", Started: today + 1},
		{Key: "n2", State: "paused", Started: today + 2},
		{Key: "w2", State: "running", Started: today + 2},
		{Key: "n1", State: "paused", Started: today + 1},
		{Key: "d2", State: "done", Started: today + 2},
	}}}
	var ids []string
	for _, it := range BuildBoard(in).Items {
		ids = append(ids, it.Ref)
	}
	if got := strings.Join(ids, " "); got != "n1 n2 w2 w1 d2 d1" {
		t.Errorf("order %q", got)
	}
}

// Spec (MOB-18): the board holds at most MaxBoardItems tiles (the rest
// are counted in More, and in Counts); a title is one line of at most
// MaxBoardText bytes, cut on a character edge; an empty laptop gives an
// empty list, never null.
func TestBuildBoardCaps(t *testing.T) {
	now, today := boardNow(t)
	var runs []RunItem
	for i := 0; i < MaxBoardItems+7; i++ {
		runs = append(runs, RunItem{Key: "r" + string(rune('a'+i%26)) + strings.Repeat("x", i/26), State: "running", Started: today + int64(i)})
	}
	runs[len(runs)-1].Title = strings.Repeat("é", MaxBoardText) + "\nsecond line"
	b := BuildBoard(BoardInput{Now: now, Runs: RunsSnap{Runs: runs}})
	if len(b.Items) != MaxBoardItems || b.More != 7 || b.Counts.Working != MaxBoardItems+7 {
		t.Errorf("items %d more %d counts %+v", len(b.Items), b.More, b.Counts)
	}
	for _, it := range b.Items {
		if len(it.Title) > MaxBoardText || !utf8.ValidString(it.Title) || strings.Contains(it.Title, "\n") {
			t.Errorf("title %q", it.Title)
		}
	}
	empty := BuildBoard(BoardInput{Now: now})
	if empty.Items == nil || len(empty.Items) != 0 || empty.Counts != (BoardCounts{}) {
		t.Errorf("empty board %+v", empty)
	}
}

// Spec (MOB-18): board.get is a read: a viewer may call it, it takes no
// args, it is rate capped and not audited.
func TestBoardGetPolicy(t *testing.T) {
	p, ok := Lookup(OpBoardGet)
	if !ok {
		t.Fatal("no board.get row")
	}
	if p.Role != RoleViewer || p.LaptopCard || p.Audit != AuditNo || p.Rate.N == 0 || p.MaxBytes > 1*kib {
		t.Errorf("row %+v", p)
	}
	if !Allowed(RoleViewer, OpBoardGet) {
		t.Error("a viewer may not read the board")
	}
	if _, err := DecodeArgs(OpBoardGet, []byte(`{"x":1}`)); err == nil {
		t.Error("board.get took an unknown arg")
	}
}
