package app

import (
	"errors"
	"strings"
	"testing"

	"termocode/internal/search"
)

func TestFormatFindGroups(t *testing.T) {
	lines := formatFindGroups([]search.Result{
		{Path: "/a.go", Line: 9, Preview: "nine"},
		{Path: "/b.go", Line: 1, Preview: "one"},
		{Path: "/a.go", Line: 2, Preview: "two"},
	})
	want := []string{
		"/a.go (2):",
		"       2:    two",
		"       9:    nine",
		"",
		"/b.go (1):",
		"       1:    one",
		"",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestFindResultsFooter(t *testing.T) {
	if got := findResultsFooter(search.Summary{Shown: 3, Total: 3, Files: 1}); got != "3 matches across 1 file" {
		t.Errorf("uncapped footer = %q", got)
	}
	got := findResultsFooter(search.Summary{Shown: 2000, Total: 5123, Files: 40})
	if !strings.HasPrefix(got, "Showing 2000 of 5123 matches across 40 files") {
		t.Errorf("capped footer = %q", got)
	}
}

func TestFormatFindResults_Shape(t *testing.T) {
	body := formatFindResults("q", []search.Result{{Path: "/a", Line: 1, Preview: "q"}})
	if !strings.HasPrefix(body, "Searching 1 file for \"q\"\n\n/a (1):\n") {
		t.Errorf("header: %q", body)
	}
	if !strings.HasSuffix(body, "\n1 match across 1 file\n") {
		t.Errorf("footer: %q", body)
	}
}

func TestSearchMaxResultsSetting(t *testing.T) {
	if got := searchMaxResults(settingsConfig{}); got != search.DefaultMaxResults {
		t.Errorf("default = %d", got)
	}
	zero, neg, n := 0, -5, 50
	if got := searchMaxResults(settingsConfig{SearchMaxResults: &zero}); got != 0 {
		t.Errorf("0 (unlimited) = %d", got)
	}
	if got := searchMaxResults(settingsConfig{SearchMaxResults: &neg}); got != search.DefaultMaxResults {
		t.Errorf("negative = %d", got)
	}
	if got := searchMaxResults(settingsConfig{SearchMaxResults: &n}); got != 50 {
		t.Errorf("50 = %d", got)
	}

	// Round-trips through config.json next to the other settings.
	withConfigDir(t)
	if err := saveSettings(settingsConfig{SearchMaxResults: &n}); err != nil {
		t.Fatal(err)
	}
	if got := searchMaxResults(loadSettings()); got != 50 {
		t.Errorf("persisted = %d", got)
	}
}

// TestHandleFindBatch_StaleAndEmpty checks that batches from a superseded
// run are dropped and an empty finished run toasts instead of opening a
// buffer.
func TestHandleFindBatch_StaleAndEmpty(t *testing.T) {
	m := Model{w: 120, h: 40}
	m.findRun = findResultsRun{gen: 2, query: "zzz"}

	if cmd := m.handleFindBatch(findBatchMsg{gen: 1, batch: search.Batch{Done: true}}); cmd != nil {
		t.Error("stale batch should be ignored")
	}
	if cmd := m.handleFindBatch(findBatchMsg{gen: 2, batch: search.Batch{Done: true}}); cmd == nil {
		t.Error("empty finished run should push a toast")
	}
	if m.findRun.opened {
		t.Error("no buffer should open without results")
	}

	m.findRun = findResultsRun{gen: 3, query: "x"}
	if cmd := m.handleFindBatch(findBatchMsg{gen: 3, batch: search.Batch{Done: true, Err: errors.New("boom")}}); cmd == nil {
		t.Error("error should push a toast")
	}
}

func TestReplaceConfirmMessage(t *testing.T) {
	counts := []search.FileCount{{Path: "/a", Count: 3}, {Path: "/b", Count: 1}}
	got := replaceConfirmMessage("foo", "bar", counts, 0)
	if got != `Replace 4 matches of "foo" with "bar" in 2 files?` {
		t.Errorf("message = %q", got)
	}
	if got := replaceConfirmMessage("foo", "bar", counts, 1); !strings.Contains(got, "1 of them has unsaved changes") {
		t.Errorf("dirty warning missing: %q", got)
	}
}
