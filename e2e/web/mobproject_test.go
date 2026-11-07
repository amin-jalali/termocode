//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The phone's project page (MOBUX-7): one project with the tabs Tasks ·
// Runs · Git · Board, against the fake hub. #/more/projects/<id>

const (
	homeProject  = "a1b2c3d4e5f6"
	otherProject = "0f0e0d0c0b0a"
)

// projectsSnap is a projects topic (MOB-8): the open one ("here"), one
// running beside it, one asleep.
func projectsSnap() map[string]any {
	return map[string]any{"state": "ready", "projects": []any{
		map[string]any{"id": homeProject, "name": "app", "where": "~/src/app", "state": "here", "attached": true, "working": 1},
		map[string]any{"id": otherProject, "name": "api", "where": "~/src/api", "state": "running", "waiting": 1},
	}}
}

// projectTasks is a project.tasks result (MOB-52): a checklist, issues
// and pull requests; one section has a note instead of items.
func projectTasks() map[string]any {
	return map[string]any{"sections": []any{
		map[string]any{"kind": "checklist", "title": "TASKS.md", "total": 3, "open": 2, "items": []any{
			map[string]any{"id": "TASKS.md:4", "kind": "ptask", "title": "Fix the login toast", "state": "open", "path": "TASKS.md", "line": 4, "group": "Now"},
			map[string]any{"id": "TASKS.md:5", "kind": "ptask", "title": "Child step", "state": "open", "path": "TASKS.md", "line": 5, "group": "Now", "depth": 1},
			map[string]any{"id": "TASKS.md:9", "kind": "ptask", "title": "Old work", "state": "done", "path": "TASKS.md", "line": 9, "group": "Done"},
		}},
		map[string]any{"kind": "issues", "title": "Issues", "total": 1, "open": 1, "items": []any{
			map[string]any{"id": "issue:41", "kind": "issue", "title": "<b>Crash</b> on start", "state": "open", "number": 41, "author": "sam", "labels": []any{"bug"}},
		}},
		map[string]any{"kind": "prs", "title": "Pull requests", "total": 1, "open": 1, "items": []any{
			map[string]any{"id": "pr:14", "kind": "pr", "title": "Rate limit login", "state": "draft", "number": 14, "checks": "fail", "branch": "rate-limit"},
		}},
		map[string]any{"kind": "mrs", "title": "Merge requests", "total": 0, "open": 0, "items": []any{}, "note": "Sign in to GitLab on the laptop."},
	}}
}

// openProject logs in on the phone, opens the project page of id and
// answers the projects topic.
func openProject(t *testing.T, id string) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-proj")
	Eval[bool](p, `(location.hash = "#/more/projects/`+id+`", true)`)
	c.WaitSub(t, "projects")
	c.Send(t, Snap("projects", 1, projectsSnap()))
	p.WaitJS(`!!document.querySelector('[data-view="project"]')`)
	return p, c
}

// tasksAnswer waits for the page's project.tasks and answers it.
func tasksAnswer(t *testing.T, c *HubConn, data any) {
	t.Helper()
	r := expectOp(t, c, "project.tasks", "")
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(data)})
}

// The page shows the project's name and four tabs, Tasks first and
// chosen; every tab is a 44 px target and a real tab (role, selected).
func TestProjectPageTabs(t *testing.T) {
	p, c := openProject(t, homeProject)
	tasksAnswer(t, c, projectTasks())
	if s := p.Text(`[data-view="project"] .view-title`); s != "app" {
		t.Errorf("title %q", s)
	}
	if s := Eval[string](p, `[...document.querySelectorAll('.ptabs [role="tab"]')].map(b => b.textContent.trim()).join("|")`); s != "Tasks|Runs|Git|Board" {
		t.Fatalf("tabs %q", s)
	}
	if s := Eval[string](p, `document.querySelector('.ptabs [aria-selected="true"]')?.textContent.trim() ?? ""`); s != "Tasks" {
		t.Errorf("chosen tab %q", s)
	}
	if n := Eval[int](p, `[...document.querySelectorAll('.ptabs [role="tab"]')].filter(b => b.getBoundingClientRect().height < 44).length`); n != 0 {
		t.Errorf("%d tabs under 44 px", n)
	}
	p.noOverflow("project page")
}

// Tasks lists each source as a section with its open count; a done item
// shows ✓ (shape and colour), a draft pull request ◐, failing checks ✗;
// a section with a note shows the note; data is text, never HTML.
func TestProjectTasksSections(t *testing.T) {
	p, c := openProject(t, homeProject)
	tasksAnswer(t, c, projectTasks())
	p.WaitJS(`document.querySelectorAll(".ptask").length === 5`)
	if s := Eval[string](p, `[...document.querySelectorAll(".psec-title")].map(e => e.textContent.trim()).join("|")`); s != "TASKS.md · 2 open|Issues · 1 open|Pull requests · 1 open|Merge requests" {
		t.Errorf("sections %q", s)
	}
	if s := p.Text(`.ptask[data-id="TASKS.md:9"] .pmark`); s != "✓" {
		t.Errorf("done mark %q", s)
	}
	if s := p.Text(`.ptask[data-id="pr:14"] .pmark`); s != "◐" {
		t.Errorf("draft mark %q", s)
	}
	if s := p.Text(`.ptask[data-id="pr:14"] .ptask-meta`); !strings.Contains(s, "#14") || !strings.Contains(s, "✗ checks") {
		t.Errorf("pr meta %q", s)
	}
	if n := Eval[int](p, `document.querySelectorAll(".ptasks b").length`); n != 0 {
		t.Error("a title was drawn as HTML")
	}
	if s := p.Text(`.psec[data-kind="mrs"] .sheet-note`); s != "Sign in to GitLab on the laptop." {
		t.Errorf("note %q", s)
	}
}

// A failed project.tasks shows a calm line with Try again, which asks again.
func TestProjectTasksRetry(t *testing.T) {
	p, c := openProject(t, homeProject)
	r := expectOp(t, c, "project.tasks", "")
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeBusy, Msg: "no answer"})
	p.WaitJS(`!!document.querySelector(".ptasks .err-line")`)
	p.TapJS(ButtonByText("Try again"))
	tasksAnswer(t, c, projectTasks())
	p.WaitJS(`document.querySelectorAll(".ptask").length === 5`)
}

// Tapping a task opens it in place with "Talk about it", which puts a
// reference (never the file's contents) in the chat box and opens Chat.
func TestProjectTaskTalkAboutIt(t *testing.T) {
	p, c := openProject(t, homeProject)
	tasksAnswer(t, c, projectTasks())
	p.TapJS(`document.querySelector('.ptask[data-id="TASKS.md:4"] .ptask-open')`)
	p.WaitJS(`document.querySelector('.ptask[data-id="TASKS.md:4"] .ptask-open').getAttribute("aria-expanded") === "true"`)
	p.TapJS(ButtonByText("Talk about it"))
	p.WaitJS(`location.hash.startsWith("#/chat")`)
}

// Runs and Git of the open project are live: each tab subscribes to its
// topic when chosen; Board counts the runs by column.
func TestProjectRunsGitBoard(t *testing.T) {
	p, c := openProject(t, homeProject)
	tasksAnswer(t, c, projectTasks())
	p.TapJS(`[...document.querySelectorAll('.ptabs [role="tab"]')][1]`)
	c.WaitSub(t, "runs")
	c.Send(t, Snap("runs", 1, webproto.RunsSnap{Runs: []webproto.RunItem{
		{Key: "c1/r1", Title: "fix the toast", State: "running", Step: 2, Steps: 3, Live: true},
		{Key: "c1/r2", Title: "write docs", State: "paused", NeedsYou: true},
		{Key: "c1/r3", Title: "old one", State: "done"},
	}}))
	p.WaitJS(`document.querySelectorAll(".pruns .run").length === 3`)
	p.TapJS(`[...document.querySelectorAll('.ptabs [role="tab"]')][2]`)
	c.WaitSub(t, "git")
	c.Send(t, Snap("git", 1, gitSnap()))
	p.WaitJS(`!!document.querySelector(".pgit .pgit-branch")`)
	if s := p.Text(".pgit .pgit-branch"); !strings.Contains(s, "main") {
		t.Errorf("branch %q", s)
	}
	p.TapJS(`[...document.querySelectorAll('.ptabs [role="tab"]')][3]`)
	p.WaitJS(`document.querySelectorAll(".pboard-col").length === 4`)
	if s := Eval[string](p, `[...document.querySelectorAll(".pboard-col .pboard-n")].map(e => e.textContent.trim()).join(",")`); s != "1,1,0,1" {
		t.Errorf("board counts %q (needs you, working, review, done)", s)
	}
	p.noOverflow("board")
}

// Another project's Runs, Git and Board say where to see them and
// subscribe to nothing; its tasks still load.
func TestProjectOtherHasTasksOnly(t *testing.T) {
	p, c := openProject(t, otherProject)
	tasksAnswer(t, c, projectTasks())
	p.TapJS(`[...document.querySelectorAll('.ptabs [role="tab"]')][1]`)
	p.WaitJS(`!!document.querySelector(".pother")`)
	c.Quiet(t, 300*time.Millisecond)
}
