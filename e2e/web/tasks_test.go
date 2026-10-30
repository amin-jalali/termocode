//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Tasks and Runs under More (WEB-25, web UX 5.8) against the fake hub.

// openMore logs in and opens More › the n-th link (0 Search, 1 Tasks, 2 Runs).
func openMore(t *testing.T, vp Viewport, n int, topic string) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	c := p.Login("c-more")
	p.GoTab("more")
	p.TapJS(`document.querySelectorAll(".more-link")[` + string(rune('0'+n)) + `]`)
	want := []string{topic}
	if vp.W >= 1200 && topic == "tasks" {
		// The desktop's Run side bar shows the tasks and the AI runs.
		want = append(want, "runs")
	}
	waitSubs(t, c, want...)
	return p, c
}

// waitSubs waits until the page has subscribed to every topic, in any order.
func waitSubs(t *testing.T, c *HubConn, topics ...string) {
	t.Helper()
	left := map[string]bool{}
	for _, x := range topics {
		left[x] = true
	}
	for len(left) > 0 {
		m := c.Next(t)
		sub, ok := m.(*webproto.Sub)
		if !ok {
			t.Fatalf("page sent %s while subs for %v were expected", show(m), topics)
		}
		for _, x := range sub.Topics {
			delete(left, x)
		}
	}
}

func tasksSnap(last *webproto.TaskLast) webproto.TasksSnap {
	return webproto.TasksSnap{Tasks: []webproto.TaskItem{
		{Label: "build", Group: "build", Default: true, Source: "tasks.json", Detail: "go build ./..."},
		{Label: "test", Group: "test", Source: "tasks.json", Detail: "go test ./...", Last: last},
	}}
}

// Run asks in the row first (Cancel first and focused; Enter cancels),
// then starts the task by label; its output shows under the row and
// reads again while it runs; Stop asks first too. No command field.
func TestTaskRunShowsOutput(t *testing.T) {
	for _, vp := range RowViews {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openMore(t, vp, 1, "tasks")
			c.Send(t, Snap("tasks", 1, tasksSnap(nil)))
			p.WaitJS(`document.querySelectorAll(".task").length === 2`)
			if txt := p.Text(".tasks-free"); txt != "Use the terminal for a command." {
				t.Errorf("free text line %q", txt)
			}
			if n := Eval[int](p, `document.querySelectorAll(".tasks input, .tasks textarea").length`); n != 0 {
				t.Fatalf("%d text fields on the tasks page", n)
			}
			run := `document.querySelector('.task[data-label="test"] .task-line .btn')`
			p.TapJS(run)
			p.WaitJS(`document.activeElement === document.querySelector(".row-ask button")`)
			if txt := p.Text(".row-ask-text"); txt != "Run test on the laptop?" {
				t.Errorf("ask %q", txt)
			}
			p.Key(kb.Enter)
			p.WaitJS(`!document.querySelector(".row-ask")`)
			c.Quiet(t, 200*time.Millisecond)
			p.TapJS(run)
			p.TapJS(`[...document.querySelectorAll(".row-ask button")].find((b) => b.textContent.trim() === "Run")`)
			r := expectOp(t, c, webproto.OpTasksRun, `{"label":"test"}`)
			reply(t, c, r, webproto.TaskStarted{Buf: 41})
			c.Send(t, Delta("tasks", 2, tasksSnap(&webproto.TaskLast{State: "running", When: time.Now().UnixMilli(), Buf: 41})))
			tail := expectOp(t, c, webproto.OpTasksTail, `{"label":"test"}`)
			reply(t, c, tail, webproto.TaskTail{Buf: 41, Running: true, Lines: []string{"ok  pkg/a", "<b>FAIL</b> pkg/b"}})
			p.WaitJS(`document.querySelectorAll(".task-rows .trow").length === 2`)
			if Eval[int](p, `document.querySelectorAll(".task-rows b").length`) != 0 {
				t.Fatal("output became HTML")
			}
			// Running: it reads again.
			reply(t, c, expectOp(t, c, webproto.OpTasksTail, `{"label":"test"}`), webproto.TaskTail{Buf: 41, Running: true, Lines: []string{"x"}})
			p.WaitJS(`document.querySelectorAll(".task-rows .trow").length === 1`)

			p.TapJS(`document.querySelector('.task[data-label="test"] .task-line .btn')`)
			p.WaitJS(`document.querySelector(".row-ask-text")?.textContent === "Stop test on the laptop?"`)
			p.TapJS(`[...document.querySelectorAll(".row-ask button")].find((b) => b.textContent.trim() === "Stop")`)
			end := time.Now().Add(wait)
			for {
				if time.Now().After(end) {
					t.Fatalf("no %s within %s", webproto.OpTasksTerminate, wait)
				}
				m := c.Next(t)
				if req, ok := m.(*webproto.Req); ok {
					if req.Op == webproto.OpTasksTail {
						reply(t, c, req, webproto.TaskTail{Buf: 41, Running: true, Lines: []string{"x"}})
						continue
					}
					if req.Op != webproto.OpTasksTerminate || canonJSON(t, req.Args) != `{"label":"test"}` {
						t.Fatalf("page sent %s", show(req))
					}
					reply(t, c, req, nil)
					break
				}
			}
			if !strings.Contains(p.Text(".task-meta"), "go") && !strings.Contains(p.Text(".task-meta"), "tasks.json") {
				t.Errorf("meta %q", p.Text(".task-meta"))
			}
		})
	}
}

// Runs show their state, step and time; Stop asks first, Pause and
// Resume follow what the run allows; a run of another chat offers none.
func TestRunsStopFromPhone(t *testing.T) {
	p, c := openMore(t, Phone, 2, "runs")
	c.Send(t, Snap("runs", 1, webproto.RunsSnap{Runs: []webproto.RunItem{
		{Key: "c1/r1", Title: "fix the toast flicker", Profile: "review", State: "running", Step: 2, Steps: 3, ElapsedMs: 41000, Live: true, Can: []string{"stop", "pause"}},
		{Key: "c2/r2", Title: "write release notes", State: "resumable", Why: "stopped at 10:02", NeedsYou: true},
	}}))
	p.WaitJS(`document.querySelectorAll(".run").length === 2`)
	meta := p.Text(`.run[data-key="c1/r1"] .run-meta`)
	if !strings.Contains(meta, "review · working now · step 2 of 3 · 4") {
		t.Errorf("meta %q", meta)
	}
	if n := Eval[int](p, `document.querySelectorAll('.run[data-key="c2/r2"] .btn').length`); n != 0 {
		t.Errorf("%d buttons on a run of another chat", n)
	}
	p.TapJS(ButtonByText("Stop"))
	p.WaitJS(`document.activeElement === document.querySelector(".row-ask button")`)
	p.TapJS(`[...document.querySelectorAll(".row-ask button")].find((b) => b.textContent.trim() === "Stop")`)
	reply(t, c, expectOp(t, c, webproto.OpRunsStop, `{"key":"c1/r1"}`), nil)
	p.TapJS(ButtonByText("Pause"))
	reply(t, c, expectOp(t, c, webproto.OpRunsPause, `{"key":"c1/r1"}`), nil)
	p.noOverflow("runs")
}

// The desktop's Tasks and AI runs view: a tree with state icons; Run
// asks in the view (Cancel first and focused; Enter cancels), then runs
// the task by label; Show Output opens its terminal in the panel; a run
// pauses at once and asks before it stops.
func TestDeskRunTasksAndRuns(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-run")
	Eval[bool](p, `(location.hash = "#/more/tasks", true)`)
	waitSubs(t, c, "tasks", "runs")
	c.Send(t, Snap("tasks", 1, tasksSnap(&webproto.TaskLast{State: "failed", Code: 2, When: time.Now().UnixMilli(), Buf: 41})))
	c.Send(t, Snap("runs", 1, webproto.RunsSnap{Runs: []webproto.RunItem{{Key: "c1/r1", Title: "fix the toast flicker", Profile: "coder", State: "running", Step: 2, Steps: 3, ElapsedMs: 41000, Live: true, Can: []string{"stop", "pause"}}}}))
	const view = `document.querySelector('.wb-side [data-view="run"]')`
	p.WaitJS(view + `?.querySelectorAll('.tr[data-label]').length === 2 && ` + view + `.querySelector('.tr[data-key]')`)
	if !Eval[bool](p, `!!`+view+`.querySelector('.tr[data-label="test"] .c-err .cod-error') && !!`+view+`.querySelector('.tr[data-label="build"] .cod-circle-outline')`) {
		t.Error("the state icons are wrong")
	}
	if n := Eval[int](p, view+`.querySelectorAll("input, textarea").length`); n != 0 {
		t.Fatalf("%d text fields in the view", n)
	}
	Eval[bool](p, view+`.querySelector('.tr[data-label="build"] [aria-label="Run Task"]').click() ?? true`)
	p.WaitJS(view + `.querySelector(".pv-ask")?.textContent.includes("Run build on the laptop?") && document.activeElement?.textContent === "Cancel"`)
	p.Key(kb.Enter)
	p.WaitJS(`!` + view + `.querySelector(".pv-ask")`)
	c.Quiet(t, 200*time.Millisecond)
	Eval[bool](p, view+`.querySelector('.tr[data-label="build"] [aria-label="Run Task"]').click() ?? true`)
	p.TapJS(`[...` + view + `.querySelectorAll(".pv-ask button")].find((b) => b.textContent === "Run")`)
	reply(t, c, expectOp(t, c, webproto.OpTasksRun, `{"label":"build"}`), webproto.TaskStarted{Buf: 42})
	// Show Output: the task's terminal in the panel.
	p.TapJS(view + `.querySelector('.tr[data-label="test"]')`)
	p.WaitJS(`location.hash === "#/term/41" && document.querySelector(".wb-panel")`)
	// The run: Pause at once; Stop asks first.
	Eval[bool](p, view+`.querySelector('.tr[data-key="c1/r1"] [aria-label="Pause"]').click() ?? true`)
	reply(t, c, expectOp(t, c, "runs.pause", `{"key":"c1/r1"}`), nil)
	Eval[bool](p, view+`.querySelector('.tr[data-key="c1/r1"] [aria-label="Stop"]').click() ?? true`)
	p.WaitJS(view + `.querySelector(".pv-ask")?.textContent.includes("Stop this run?")`)
	p.TapJS(`[...` + view + `.querySelectorAll(".pv-ask button")].find((b) => b.textContent === "Stop")`)
	reply(t, c, expectOp(t, c, "runs.stop", `{"key":"c1/r1"}`), nil)
}
