//go:build browser

package web

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The phone's project page (MOBUX-7): one project with the tabs Tasks ·
// Runs · Git · Board, against the fake hub. #/more/projects/<id>; the
// list (MOBUX-6) is #/more/projects. Another project's frames carry its
// workspace key in ws (the door, MOB-50).

const (
	homeProject  = "a1b2c3d4e5f6"
	otherProject = "0f0e0d0c0b0a"
	otherWS      = "0f0e0d0c0b0a0908"
)

// projectsSnap is a projects topic (MOB-8): the open one ("here") and
// one running beside it, reachable through the door by its ws.
func projectsSnap() webproto.ProjectsSnap {
	return webproto.ProjectsSnap{State: "ready", Projects: []webproto.ProjectItem{
		{ID: homeProject, Name: "app", Where: "~/src/app", State: "here", Attached: true, Working: 1},
		{ID: otherProject, Name: "api", Where: "~/src/api", State: "running", Waiting: 1, WS: otherWS},
	}}
}

// projectTasks is a project.tasks result (MOB-52): a checklist, issues
// and pull requests; one section has a note instead of items.
func projectTasks() webproto.ProjectTasks {
	return webproto.ProjectTasks{Sections: []webproto.ProjectTaskSection{
		{Kind: "checklist", Title: "TASKS.md", Total: 3, Open: 2, Items: []webproto.ProjectTask{
			{ID: "TASKS.md:4", Kind: "ptask", Title: "Fix the login toast", State: "open", Path: "TASKS.md", Line: 4, Group: "Now"},
			{ID: "TASKS.md:5", Kind: "ptask", Title: "Child step", State: "open", Path: "TASKS.md", Line: 5, Group: "Now", Depth: 1},
			{ID: "TASKS.md:9", Kind: "ptask", Title: "Old work", State: "done", Path: "TASKS.md", Line: 9, Group: "Done"},
		}},
		{Kind: "issues", Title: "Issues", Total: 1, Open: 1, Items: []webproto.ProjectTask{
			{ID: "issue:41", Kind: "issue", Title: "<b>Crash</b> on start", State: "open", Number: 41, Author: "sam", Labels: []string{"bug"}},
		}},
		{Kind: "prs", Title: "Pull requests", Total: 1, Open: 1, Items: []webproto.ProjectTask{
			{ID: "pr:14", Kind: "pr", Title: "Rate limit login", State: "draft", Number: 14, Checks: "fail", Branch: "rate-limit"},
		}},
		{Kind: "mrs", Title: "Merge requests", Items: []webproto.ProjectTask{}, Note: "Sign in to GitLab on the laptop."},
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

// tasksAnswer waits for the page's project.tasks, checks the frame's ws
// and answers it.
func tasksAnswer(t *testing.T, c *HubConn, ws string, data any) {
	t.Helper()
	r := expectOp(t, c, webproto.OpProjectTasks, "")
	if r.WS != ws {
		t.Errorf("project.tasks went to ws %q; want %q", r.WS, ws)
	}
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(data)})
}

// tab taps the n-th project tab (0 Tasks, 1 Runs, 2 Git, 3 Board).
func tab(p *Page, n int) {
	p.T.Helper()
	p.TapJS(`[...document.querySelectorAll('.ptabs [role="tab"]')][` + string(rune('0'+n)) + `]`)
}

// The list opens a project: a card tap goes to its page.
func TestProjectsListOpensThePage(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	c := p.Login("c-projs")
	Eval[bool](p, `(location.hash = "#/more/projects", true)`)
	c.WaitSub(t, "projects")
	c.Send(t, Snap("projects", 1, projectsSnap()))
	p.WaitJS(`document.querySelectorAll(".ph-proj").length === 2`)
	p.TapJS(`document.querySelector('.ph-proj[data-key="` + otherProject + `"]')`)
	p.WaitJS(`location.hash === "#/more/projects/` + otherProject + `"`)
	p.WaitJS(`document.querySelector('[data-view="project"] .view-title')?.textContent === "api"`)
}

// The page shows the project's name and four tabs, Tasks first and
// chosen; every tab is a 44 px target and a real tab (role, selected).
func TestProjectPageTabs(t *testing.T) {
	p, c := openProject(t, homeProject)
	tasksAnswer(t, c, "", projectTasks())
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

// Tasks is the project.tasks view: every task a row, data as text,
// never HTML; a section's note shows.
func TestProjectTasksRows(t *testing.T) {
	p, c := openProject(t, homeProject)
	tasksAnswer(t, c, "", projectTasks())
	p.WaitJS(`document.querySelectorAll(".ppanel .ph-task").length === 5`)
	if n := Eval[int](p, `document.querySelectorAll(".ppanel .ph-tasks b").length`); n != 0 {
		t.Error("a title was drawn as HTML")
	}
	if !strings.Contains(p.Text(".ppanel .ph-tasks"), "Sign in to GitLab on the laptop.") {
		t.Error("the section note is missing")
	}
}

// Tapping a task opens a sheet, Cancel first and focused; "Talk about
// it" puts a reference (never the file's contents) in the chat box and
// opens Chat.
func TestProjectTaskTalkAboutIt(t *testing.T) {
	p, c := openProject(t, homeProject)
	tasksAnswer(t, c, "", projectTasks())
	p.WaitJS(`document.querySelectorAll(".ppanel .ph-task").length === 5`)
	p.TapJS(`document.querySelector(".ppanel .ph-task button")`)
	p.WaitJS(`!!document.querySelector(".sheet .ph-sheet-act")`)
	if s := Eval[string](p, `[...document.querySelectorAll(".sheet .ph-sheet-act")].map(b => b.textContent.trim()).join("|")`); s != "Cancel|Talk about it" {
		t.Errorf("actions %q", s)
	}
	p.WaitJS(`document.activeElement?.textContent.trim() === "Cancel"`)
	p.TapJS(ButtonByText("Talk about it"))
	p.WaitJS(`location.hash.startsWith("#/chat")`)
}

// Runs and Git of the open project are live: each tab subscribes to its
// topic when chosen; Board counts the runs by column.
func TestProjectRunsGitBoard(t *testing.T) {
	p, c := openProject(t, homeProject)
	tasksAnswer(t, c, "", projectTasks())
	tab(p, 1)
	c.WaitSub(t, "runs")
	c.Send(t, Snap("runs", 1, webproto.RunsSnap{Runs: []webproto.RunItem{
		{Key: "c1/r1", Title: "fix the toast", State: "running", Step: 2, Steps: 3, Live: true},
		{Key: "c1/r2", Title: "write docs", State: "paused", NeedsYou: true},
		{Key: "c1/r3", Title: "old one", State: "done"},
	}}))
	p.WaitJS(`document.querySelectorAll(".pruns .run").length === 3`)
	tab(p, 2)
	c.WaitSub(t, "git")
	c.Send(t, Snap("git", 1, gitSnap()))
	p.WaitJS(`!!document.querySelector(".pgit .pgit-branch")`)
	if s := p.Text(".pgit .pgit-branch"); !strings.Contains(s, "main") {
		t.Errorf("branch %q", s)
	}
	tab(p, 3)
	p.WaitJS(`document.querySelectorAll(".pboard-col").length === 4`)
	if s := Eval[string](p, `[...document.querySelectorAll(".pboard-col .pboard-n")].map(e => e.textContent.trim()).join(",")`); s != "1,1,0,1" {
		t.Errorf("board counts %q (needs you, working, review, done)", s)
	}
	p.noOverflow("board")
}

// Another project goes through the door: its project.tasks and its runs
// subscription carry its ws, and its runs show from its own data.
func TestProjectOtherThroughTheDoor(t *testing.T) {
	p, c := openProject(t, otherProject)
	tasksAnswer(t, c, otherWS, projectTasks())
	tab(p, 1)
	for {
		m := c.Next(t)
		sub, ok := m.(*webproto.Sub)
		if !ok {
			t.Fatalf("page sent %s; want a sub for the other project's runs", show(m))
		}
		if len(sub.Topics) == 1 && sub.Topics[0] == "runs" {
			if sub.WS != otherWS {
				t.Fatalf("runs sub went to ws %q; want %q", sub.WS, otherWS)
			}
			break
		}
	}
	snap := Snap("runs", 1, webproto.RunsSnap{Runs: []webproto.RunItem{{Key: "c9/r1", Title: "api run", State: "running", Live: true}}})
	snap.WS = otherWS
	c.Send(t, snap)
	p.WaitJS(`document.querySelectorAll(".pruns .run").length === 1`)
}
