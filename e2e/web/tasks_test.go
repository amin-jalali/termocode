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
	c.WaitSub(t, topic)
	return p, c
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
	for _, vp := range Both {
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
			for {
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
