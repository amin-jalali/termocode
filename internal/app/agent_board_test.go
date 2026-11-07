package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentboard"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

func TestBoardRunCardColumns(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	base := runsItem{key: "c1/r1", task: "fix the toast flicker", chatTitle: "toast", run: ai.StoredRun{Profile: "review"}, started: now.Add(-time.Hour)}
	cases := []struct {
		kind   runsKind
		resume int
		col    agentboard.Column
		can    bool
	}{
		{runsRunning, 0, agentboard.Working, false},
		{runsStalled, 0, agentboard.NeedsYou, false},
		{runsPaused, 0, agentboard.NeedsYou, true},
		{runsResumable, 2, agentboard.NeedsYou, true},
		{runsFailed, 0, agentboard.NeedsYou, false},
		{runsDone, 0, agentboard.Review, false},
		{runsCancelled, 0, agentboard.Done, false},
	}
	for _, c := range cases {
		it := base
		it.kind, it.resume = c.kind, c.resume
		card := boardRunCard(it, now)
		if card.Col != c.col || card.Resume != c.can {
			t.Errorf("kind %d: col %v resume %v, want %v %v", c.kind, card.Col, card.Resume, c.col, c.can)
		}
		if card.ID != "run:c1/r1" || card.Source != agentboard.SourceRun || card.Title != "fix the toast flicker" {
			t.Errorf("kind %d: card %+v", c.kind, card)
		}
		if !strings.Contains(card.Where, "review") {
			t.Errorf("kind %d: where %q has no profile", c.kind, card.Where)
		}
	}
	old := base
	old.kind, old.started = runsDone, now.Add(-agentboard.ReviewFor-time.Hour)
	if got := boardRunCard(old, now).Col; got != agentboard.Done {
		t.Errorf("an old done run is in %v, want Done", got)
	}
}

func TestBoardLaneCard(t *testing.T) {
	e := worktrees.Entry{Worktree: git.Worktree{Path: "/src/app.worktrees/wt-sidebar", Branch: "feat/sidebar"}, Kind: worktrees.KindUser}
	idle := wtItem{e: e, state: wtStateIdle}
	if _, ok := boardLaneCard(idle); ok {
		t.Fatal("an idle worktree with no PR is on the board")
	}
	waiting := wtItem{e: e, state: wtStateWaiting, agents: 1, agent: wtAgent{name: "Claude Code", state: wtStateWaiting, tab: 3}}
	c, ok := boardLaneCard(waiting)
	if !ok || c.Col != agentboard.NeedsYou || c.ID != "tab:3" || c.Source != agentboard.SourceLane {
		t.Fatalf("waiting lane: %+v ok=%v", c, ok)
	}
	if !strings.Contains(c.Why, "Claude Code") {
		t.Errorf("waiting lane why %q has no agent name", c.Why)
	}
	other := waiting
	other.agent.tab = -1
	if c, _ := boardLaneCard(other); c.ID != "wt:/src/app.worktrees/wt-sidebar" {
		t.Errorf("another window's agent: id %q, want the worktree", c.ID)
	}
	red := idle
	red.e.Meta.PR = 12
	red.e.Meta.Link = worktrees.LinkInfo{State: "open", CI: "fail"}
	if c, ok := boardLaneCard(red); !ok || c.Col != agentboard.NeedsYou || c.CI != "fail" {
		t.Fatalf("red CI lane: %+v ok=%v", c, ok)
	}
	issueOnly := idle
	issueOnly.e.Meta.Issue = 4
	issueOnly.e.Meta.Link = worktrees.LinkInfo{State: "open"}
	if _, ok := boardLaneCard(issueOnly); ok {
		t.Error("a linked issue (no PR) put an idle lane on the board")
	}
	arch := idle
	arch.state = wtStateArchived
	arch.e.Meta.PR = 3
	arch.e.Meta.Link = worktrees.LinkInfo{State: "merged"}
	if _, ok := boardLaneCard(arch); ok {
		t.Error("an archived worktree is on the board")
	}
}

func TestAgentBoardRowsGroupByColumn(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	cards := []agentboard.Card{
		{ID: "run:a", Col: agentboard.Review, Title: "done run", Why: "done", At: now.Add(-time.Hour)},
		{ID: "tab:1", Col: agentboard.NeedsYou, Title: "wt-sidebar", Why: "waits for you", At: now},
		{ID: "wt:/x", Col: agentboard.NeedsYou, Title: "wt-x", Why: "CI failed", CI: "fail", At: now.Add(-time.Minute)},
	}
	rows := agentBoardRows(cards, now)
	var got []string
	for _, r := range rows {
		if r.Header {
			got = append(got, "#"+r.Title)
		} else {
			got = append(got, r.ID)
		}
	}
	want := "#NEEDS YOU · 2 tab:1 wt:/x #REVIEW · 1 run:a"
	if strings.Join(got, " ") != want {
		t.Fatalf("rows = %v, want %s", got, want)
	}
	if rows[1].Group != rows[0].Group || rows[1].Group == rows[3].Group {
		t.Error("rows do not share their column's group")
	}
	if !strings.Contains(rows[1].Hint, "waits for you") || !strings.Contains(rows[1].Hint, "now") {
		t.Errorf("hint %q", rows[1].Hint)
	}
}

func TestBoardItermocID(t *testing.T) {
	row := imRow{kind: imRowService, sec: imSecServices, name: "linear:prod"}
	id := boardImID(row)
	back, ok := boardImRow(id)
	if !ok || back != row {
		t.Fatalf("boardImRow(%q) = %+v %v, want %+v", id, back, ok, row)
	}
	for _, bad := range []string{"im:", "im:x:y", "run:a", "im:99"} {
		if _, ok := boardImRow(bad); ok {
			t.Errorf("boardImRow(%q) ok", bad)
		}
	}
}

func TestAgentBoardPolicyAndPalette(t *testing.T) {
	p, ok := aiActionPolicyFor("ai-board")
	if !ok || p.Gate != aiGateNever {
		t.Fatalf("ai-board policy = %+v %v, want never", p, ok)
	}
}
