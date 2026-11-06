package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// wfxGoldenRig is wfxRig with two stored runs of mine (a dry run and a
// real run whose write was denied), times in UTC.
func wfxGoldenRig(t *testing.T) *Model {
	t.Helper()
	prev := wfxLoc
	wfxLoc = time.UTC
	t.Cleanup(func() { wfxLoc = prev })
	m, _ := wfxRig(t)
	at := wfxTestNow.Add(-20 * time.Minute)
	items := []run.Item{{Key: "docs/a.md"}, {Key: "docs/b.md"}}
	spec := workflow.WriteSpec{To: "file", Path: "reports/docs.md", Content: "2 docs"}
	dry := &run.RunFile{Version: 1, ID: "20261106-092100", Workflow: "mine", Dry: true, State: run.RunOK,
		StartedAt: at, EndedAt: at.Add(2 * time.Second),
		Steps:  map[string]*run.StepState{"docs": {State: run.StateDone, Items: items}, "rep": {State: run.StateRecorded}},
		Writes: []run.WriteRecord{{Step: "rep", To: "file", State: run.StateRecorded, Spec: spec}}}
	real := &run.RunFile{Version: 1, ID: "20261106-093000", Workflow: "mine", State: run.RunOK,
		StartedAt: at.Add(9 * time.Minute), EndedAt: at.Add(10 * time.Minute),
		Steps:  map[string]*run.StepState{"docs": {State: run.StateDone, Items: items}, "rep": {State: run.StateDenied, Error: "the write was denied"}},
		Writes: []run.WriteRecord{{Step: "rep", To: "file", State: run.StateDenied, Spec: spec}}}
	m.wfxPutRun(dry)
	m.wfxPutRun(real)
	return m
}

// Spec (WFX-7): every screen of the view at 80×24 and 120×40 matches its
// golden (UPDATE_GOLDEN=1 writes them).
func TestWorkflowsGoldens(t *testing.T) {
	m := wfxGoldenRig(t)
	screens := []struct{ name, detail string }{
		{"sidebar", "-"}, {"overview", ""}, {"workflow", "mine"}, {"untrusted", "docs-report"},
		{"broken", "broken"}, {"dryrun", "run:20261106-092100"}, {"run", "run:20261106-093000"},
	}
	for _, sz := range [][2]int{{80, 24}, {120, 40}} {
		for _, s := range screens {
			var got string
			if s.detail == "-" {
				got = wfxSide(m, min(sz[0]/3, 40), sz[1])
			} else {
				m.wfxS().detail = s.detail
				got = wfxPage(m, sz[0], sz[1])
			}
			got = strings.Join(trimRight(strings.Split(got, "\n")), "\n") + "\n"
			path := filepath.Join("testdata", "workflows", fmt.Sprintf("%s_%dx%d.golden", s.name, sz[0], sz[1]))
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (UPDATE_GOLDEN=1 writes it)", err)
			}
			if string(want) != got {
				t.Errorf("%s differs:\n--- got\n%s--- want\n%s", path, got, want)
			}
		}
	}
}

func trimRight(lines []string) []string {
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return lines
}

// Spec: under 120 columns the view is one full-width page (no sidebar).
func TestWorkflowsNarrowMode(t *testing.T) {
	m, _ := wfxRig(t)
	m.w, m.h = 80, 24
	m.showExp = true
	m.revealWorkflows(true)
	if m.showExp || !m.wfxS().pageOn {
		t.Fatalf("narrow: sidebar %v page %v", m.showExp, m.wfxS().pageOn)
	}
}

// Perf guard (WFX-7): the page and the list draw in under 8 ms with 50
// workflows × 200 runs.
func TestWorkflowsRenderPerf(t *testing.T) {
	m, _ := wfxRig(t)
	st := m.wfxS()
	base, _ := wire.Find(st.list, "mine")
	st.list = nil
	for i := 0; i < 50; i++ {
		e := base
		e.Name = fmt.Sprintf("wf-%02d", i)
		st.list = append(st.list, e)
		var runs []*run.RunFile
		for j := 0; j < 200; j++ {
			runs = append(runs, &run.RunFile{ID: fmt.Sprintf("r%02d-%03d", i, j), Workflow: e.Name, State: run.RunOK,
				StartedAt: wfxTestNow.Add(-time.Duration(i*200+j) * time.Minute), EndedAt: wfxTestNow,
				Steps: map[string]*run.StepState{"docs": {State: run.StateDone}}})
		}
		st.runs[e.Name] = runs
	}
	st.sel = "wf-00"
	const n = 20
	start := time.Now()
	for i := 0; i < n; i++ {
		st.detail = ""
		_ = m.renderWorkflowsPage(120, 40)
		st.detail = "wf-00"
		_ = m.renderWorkflowsPage(120, 40)
		_ = m.renderWorkflowsSidebar(40, 40)
	}
	if per := time.Since(start) / (3 * n); per > 8*time.Millisecond && !raceEnabled {
		t.Fatalf("a frame takes %v (budget 8 ms)", per)
	}
}
