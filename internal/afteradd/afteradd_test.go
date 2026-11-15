package afteradd

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func findingTexts(fs []Finding) string {
	var b []string
	for _, f := range fs {
		b = append(b, f.Check+"="+f.Text)
	}
	return strings.Join(b, "; ")
}

// Every check runs; the findings come back in the rule's order, not the
// order the checks finish; a check that fails or runs out of time is a
// warning finding, never a lost one.
func TestRunChecksInOrderWithTimeoutsAndErrors(t *testing.T) {
	var started atomic.Int32
	slow := Check{Name: "slow", Timeout: 20 * time.Millisecond, Run: func(ctx context.Context, _ Event) ([]Finding, error) {
		started.Add(1)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	fast := Check{Name: "fast", Run: func(context.Context, Event) ([]Finding, error) {
		started.Add(1)
		return []Finding{{Text: "3 tools"}}, nil
	}}
	bad := Check{Name: "bad", Run: func(context.Context, Event) ([]Finding, error) {
		started.Add(1)
		return nil, errors.New("refused")
	}}
	boom := Check{Name: "boom", Run: func(context.Context, Event) ([]Finding, error) {
		started.Add(1)
		panic("bug")
	}}
	e := New(nil, Rule{Kind: KindMCP, Checks: []Check{slow, fast, bad, boom}})
	res := e.Run(context.Background(), Event{Kind: KindMCP, Item: "github"})
	if started.Load() != 4 {
		t.Fatalf("ran %d checks", started.Load())
	}
	got := findingTexts(res.Findings)
	want := "slow=did not answer in 20ms; fast=3 tools; bad=could not check: refused; boom=could not check: the check failed"
	if got != want {
		t.Fatalf("findings\n got %s\nwant %s", got, want)
	}
	for i, lv := range []Level{Warn, Info, Warn, Warn} {
		if res.Findings[i].Level != lv {
			t.Errorf("finding %d level %v, want %v", i, res.Findings[i].Level, lv)
		}
	}
	if res.Suggestion != nil {
		t.Errorf("no Suggest func, but a suggestion: %+v", res.Suggestion)
	}
}

// The suggestion sees every finding; "Not now" on it is remembered per
// item (in a file), so the same item does not ask again while another
// item still does. An event of a kind with no rule finds nothing.
func TestSuggestionAndNotNowPerItem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "later.json")
	rule := Rule{Kind: KindDB,
		Checks: []Check{{Name: "tables", Run: func(_ context.Context, ev Event) ([]Finding, error) {
			return []Finding{{Text: "12 tables", Facts: map[string]string{"n": "12"}}}, nil
		}}},
		Suggest: func(ev Event, fs []Finding) *Suggestion {
			if len(fs) == 0 || fs[0].Facts["n"] != "12" {
				return nil
			}
			return &Suggestion{Title: "Open the tables of " + ev.Item + "?", Action: Action{ID: "tables", Label: "Open tables", Op: "db-open"}}
		},
	}
	e := New(OpenLater(path), rule)
	res := e.Run(context.Background(), Event{Kind: KindDB, Item: "shop"})
	s := res.Suggestion
	if s == nil || s.Title != "Open the tables of shop?" || s.Event.Item != "shop" || s.Key() != "db:shop|tables" {
		t.Fatalf("suggestion %+v", s)
	}
	if err := e.NotNow(*s); err != nil {
		t.Fatal(err)
	}
	if res := e.Run(context.Background(), Event{Kind: KindDB, Item: "shop"}); res.Suggestion != nil || len(res.Findings) != 1 {
		t.Fatalf("after Not now: %+v", res)
	}
	if res := e.Run(context.Background(), Event{Kind: KindDB, Item: "blog"}); res.Suggestion == nil {
		t.Fatal("another item lost its suggestion")
	}
	// A new run of termocode reads the same file.
	e2 := New(OpenLater(path), rule)
	if res := e2.Run(context.Background(), Event{Kind: KindDB, Item: "shop"}); res.Suggestion != nil {
		t.Fatal("Not now forgotten after a restart")
	}
	if res := e2.Run(context.Background(), Event{Kind: KindKube, Item: "prod"}); res.Suggestion != nil || len(res.Findings) != 0 {
		t.Fatalf("no rule, but %+v", res)
	}
}

// A broken or missing later file is an empty list (it never blocks a
// card), and a nil Later remembers in memory only.
func TestLaterFileBrokenOrMissing(t *testing.T) {
	dir := t.TempDir()
	l := OpenLater(filepath.Join(dir, "nope", "later.json"))
	if l.Has("x") {
		t.Fatal("missing file has x")
	}
	if err := l.Add("x"); err != nil {
		t.Fatal(err)
	}
	if !OpenLater(filepath.Join(dir, "nope", "later.json")).Has("x") {
		t.Fatal("not saved")
	}
	var nl *Later
	if nl.Has("x") || nl.Add("x") != nil {
		t.Fatal("nil Later")
	}
}

// The whole run has a ceiling: a check with a long timeout still stops
// when the caller's context ends.
func TestRunStopsWithTheCallerContext(t *testing.T) {
	e := New(nil, Rule{Kind: KindSSH, Checks: []Check{{Name: "scan", Timeout: time.Hour, Run: func(ctx context.Context, _ Event) ([]Finding, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}}})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan Result, 1)
	go func() { done <- e.Run(ctx, Event{Kind: KindSSH, Item: "web1"}) }()
	select {
	case res := <-done:
		if len(res.Findings) != 1 || res.Findings[0].Level != Warn {
			t.Fatalf("%+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}
