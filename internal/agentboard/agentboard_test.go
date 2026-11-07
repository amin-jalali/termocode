package agentboard

import (
	"testing"
	"time"
)

func TestLaneColumn(t *testing.T) {
	cases := []struct {
		name                 string
		agent, check, pr, ci string
		want                 Column
		why                  string
		on                   bool
	}{
		{"waiting agent", "waiting", "", "", "", NeedsYou, "waits for you", true},
		{"red CI on an open PR", "working", "", "open", "fail", NeedsYou, "CI failed", true},
		{"blocked checkpoint", "", "blocked", "", "", NeedsYou, "blocked", true},
		{"working agent", "working", "", "", "", Working, "working", true},
		{"merged PR", "", "done", "merged", "pass", Done, "merged", true},
		{"closed PR", "", "", "closed", "", Done, "PR closed", true},
		{"red CI on a merged PR is done", "", "", "merged", "fail", Done, "merged", true},
		{"done agent", "done", "", "", "", Review, "done · review it", true},
		{"done with CI running", "done", "", "open", "pending", Review, "done · review it · CI running", true},
		{"open PR, green CI", "", "", "open", "pass", Review, "PR open · CI green", true},
		{"working checkpoint only", "", "working", "", "", Working, "working (checkpoint)", true},
		{"idle lane is not on the board", "", "", "", "", 0, "", false},
	}
	for _, c := range cases {
		col, why, on := LaneColumn(c.agent, c.check, c.pr, c.ci)
		if on != c.on || (on && (col != c.want || why != c.why)) {
			t.Errorf("%s: got (%v, %q, %v), want (%v, %q, %v)", c.name, col, why, on, c.want, c.why, c.on)
		}
	}
}

func TestRunColumn(t *testing.T) {
	cases := []struct {
		st   RunState
		age  time.Duration
		want Column
	}{
		{RunRunning, 0, Working},
		{RunStalled, 0, NeedsYou},
		{RunPaused, 0, NeedsYou},
		{RunResumable, time.Hour, NeedsYou},
		{RunFailed, 3 * 24 * time.Hour, NeedsYou},
		{RunDone, time.Hour, Review},
		{RunDone, ReviewFor + time.Minute, Done},
		{RunCancelled, time.Minute, Done},
	}
	for _, c := range cases {
		if got := RunColumn(c.st, c.age); got != c.want {
			t.Errorf("RunColumn(%v, %v) = %v, want %v", c.st, c.age, got, c.want)
		}
	}
}

func TestSortColumnsThenNewest(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	cards := []Card{
		{ID: "done", Col: Done, At: now},
		{ID: "old-need", Col: NeedsYou, At: now.Add(-time.Hour)},
		{ID: "work", Col: Working, At: now},
		{ID: "new-need", Col: NeedsYou, At: now},
		{ID: "review", Col: Review, At: now},
	}
	Sort(cards)
	want := []string{"new-need", "old-need", "work", "review", "done"}
	for i, id := range want {
		if cards[i].ID != id {
			t.Fatalf("order %d = %q, want %q (all: %v)", i, cards[i].ID, id, cards)
		}
	}
}

func TestMatchEveryWord(t *testing.T) {
	c := Card{Source: SourceLane, Col: NeedsYou, Title: "wt-sidebar", Where: "feat/sidebar", Why: "CI failed", CI: "fail"}
	for _, q := range []string{"", "sidebar", "SIDEBAR ci", "needs", "lane fail", "feat/side"} {
		if !Match(c, q) {
			t.Errorf("Match(%q) = false, want true", q)
		}
	}
	for _, q := range []string{"run", "sidebar working", "toast"} {
		if Match(c, q) {
			t.Errorf("Match(%q) = true, want false", q)
		}
	}
	got := Filter([]Card{c, {Title: "toast flicker", Source: SourceRun}}, "toast")
	if len(got) != 1 || got[0].Title != "toast flicker" {
		t.Fatalf("Filter(toast) = %v", got)
	}
}

func TestCountsAndSummary(t *testing.T) {
	cards := []Card{{Col: NeedsYou}, {Col: NeedsYou}, {Col: Working}, {Col: Review}, {Col: Done}}
	n := Counts(cards)
	if n[NeedsYou] != 2 || n[Working] != 1 || n[Review] != 1 || n[Done] != 1 {
		t.Fatalf("Counts = %v", n)
	}
	if got := Summary(n); got != "2 need you · 1 working · 1 to review · 1 done" {
		t.Fatalf("Summary = %q", got)
	}
	if got := Summary(Counts([]Card{{Col: NeedsYou}})); got != "1 needs you" {
		t.Fatalf("Summary(one) = %q", got)
	}
	if got := Summary(Counts(nil)); got != "nothing yet" {
		t.Fatalf("Summary(empty) = %q", got)
	}
}

func TestColumnTitles(t *testing.T) {
	want := []string{"NEEDS YOU", "WORKING", "REVIEW", "DONE"}
	for c := NeedsYou; c < ColumnCount; c++ {
		if c.Title() != want[c] {
			t.Errorf("Title(%d) = %q, want %q", c, c.Title(), want[c])
		}
	}
}

func TestAgo(t *testing.T) {
	cases := map[time.Duration]string{
		0:                 "now",
		30 * time.Second:  "now",
		5 * time.Minute:   "5 min",
		3 * time.Hour:     "3 h",
		50 * time.Hour:    "2 d",
		-10 * time.Minute: "now",
	}
	for d, want := range cases {
		if got := Ago(d); got != want {
			t.Errorf("Ago(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestCIWord(t *testing.T) {
	cases := map[string]string{"pass": "CI green", "fail": "CI failed", "pending": "CI running", "": ""}
	for ci, want := range cases {
		if got := CIWord(ci); got != want {
			t.Errorf("CIWord(%q) = %q, want %q", ci, got, want)
		}
	}
}
