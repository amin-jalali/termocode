package bench

import (
	"strings"
	"testing"
	"time"
)

func TestRunAllCollectsResultsIdleAndSkips(t *testing.T) {
	ghost := Editor{ID: "ghost", Name: "Ghost", Bins: []string{"definitely-not-installed-xyz"}}
	var logs []string
	d := RunAll(RunOpts{
		Editors: []Editor{fakeEditor(t), ghost},
		Runs:    3, Warmup: 1, IdleFor: 300 * time.Millisecond,
		Machine: "unit test", Date: "2026-11-13",
	}, func(s string) { logs = append(logs, s) })

	if len(d.Editors) != 1 || d.Editors[0].ID != "fake" || !strings.Contains(d.Editors[0].Version, "1.2.3") {
		t.Fatalf("editors %+v", d.Editors)
	}
	got := map[Measure]int{}
	for _, r := range d.Results {
		got[r.Measure] = len(r.Samples)
	}
	if got[MOpen] != 3 || got[MType] != 12 || got[MSearch] != 3 {
		t.Fatalf("samples per measure %v", got)
	}
	if len(d.Idle) != 1 || d.Idle[0].Editor != "fake" || d.Idle[0].RSSKB <= 0 {
		t.Fatalf("idle %+v", d.Idle)
	}
	skips := strings.Join(d.Skipped, "\n")
	for _, want := range []string{"Ghost: not installed", "fake / quick_open: no picker"} {
		if !strings.Contains(skips, want) {
			t.Errorf("skips lack %q:\n%s", want, skips)
		}
	}
	if d.Meta.Runs != 3 || d.Meta.Warmup != 1 || d.Meta.Machine != "unit test" {
		t.Fatalf("meta %+v", d.Meta)
	}
	if len(logs) == 0 {
		t.Fatal("progress must be logged")
	}
}

func TestRunAllRecordsAFailedMeasureInsteadOfDroppingIt(t *testing.T) {
	e := fakeEditor(t)
	e.Scripts[MSearch] = func(Env) Script {
		return Script{ReadyText: "line 0000001", Timed: Step{Keys: "x\n", Want: "never"}, Timeout: 200 * time.Millisecond}
	}
	d := RunAll(RunOpts{Editors: []Editor{e}, Runs: 1, Measures: []Measure{MOpen, MSearch}}, nil)
	skips := strings.Join(d.Skipped, "\n")
	if !strings.Contains(skips, "fake / search: failed") {
		t.Fatalf("a failed measure must be listed:\n%s", skips)
	}
	for _, r := range d.Results {
		if r.Measure == MSearch {
			t.Fatal("no result row for a failed measure")
		}
	}
	if len(d.Results) != 1 || d.Results[0].Measure != MOpen {
		t.Fatalf("the other measures still run: %+v", d.Results)
	}
}

func TestRunAllNamesGUIEditorsOnceWhenInstalled(t *testing.T) {
	e := fakeEditor(t)
	e.ID, e.Name, e.Kind = "gui", "Gui", GUI
	e.Scripts = nil
	e.Unsupported = map[Measure]string{MOpen: "GUI measure: see gui.go"}
	d := RunAll(RunOpts{Editors: []Editor{e}, Runs: 1}, nil)
	n := 0
	for _, s := range d.Skipped {
		if strings.HasPrefix(s, "Gui") {
			n++
		}
	}
	if n != 1 || len(d.Results) != 0 || len(d.Idle) != 0 {
		t.Fatalf("skipped=%v results=%d idle=%d", d.Skipped, len(d.Results), len(d.Idle))
	}
}
