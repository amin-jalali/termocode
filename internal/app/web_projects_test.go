//go:build unix

package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentroll"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workspace"
)

var (
	projT0 = time.UnixMilli(1_690_000_000_000)
	projT1 = time.UnixMilli(1_700_000_000_000)
	projT2 = time.UnixMilli(1_710_000_000_000)
)

func projByName(s webproto.ProjectsSnap, name string) (webproto.ProjectItem, bool) {
	for _, p := range s.Projects {
		if p.Name == name {
			return p, true
		}
	}
	return webproto.ProjectItem{}, false
}

func projNames(s webproto.ProjectsSnap) string {
	var out []string
	for _, p := range s.Projects {
		out = append(out, p.Name)
	}
	return strings.Join(out, ",")
}

// Spec (MOB-8): the projects come from the recent workspaces, the running
// sessions, the service folders and the agent roll-ups, one row per
// folder (a trailing slash is the same folder); this window's project
// comes first, then the ones whose agents wait for the user, then the
// working ones, the running ones and the newest. Where is ~-relative
// (outside home: the folder name only); no absolute path goes out; the
// counts add up the roll-ups.
func TestProjectsMergeDedupesRanksAndHidesPaths(t *testing.T) {
	in := projectsInput{
		recent: []WorkspaceEntry{
			{Path: "/home/u/src/app", OpenedAt: projT1},
			{Path: "/home/u/src/api/", OpenedAt: projT0},
			{Path: "/home/u/src/api", OpenedAt: projT2},
			{Path: "/opt/notes", OpenedAt: projT0},
			{Path: ""},
			{Path: "relative/x"},
		},
		sessions: []session.Info{{Workspace: "/srv/web", PID: 7}, {Workspace: "/home/u/src/app", PID: 8, Attached: true}},
		services: []string{"/home/u/src/api"},
		others: []agentroll.Rollup{
			{Workspace: "/home/u/src/api", PID: 9, Items: []agentroll.Item{{Name: "codex", State: "working"}, {Name: "claude", State: "waiting"}}},
			{Workspace: "/home/u/src/app", PID: 10, Items: []agentroll.Item{{Name: "AI chat", State: "waiting"}}},
			{Workspace: "/srv/idle", PID: 11, Items: []agentroll.Item{{Name: "old", State: "done"}}},
		},
	}
	own := []agentroll.Item{{Name: "AI chat", State: "working"}}
	s := projectsMerge("/home/u/src/app", own, &in, "/home/u")
	if s.State != "ready" || projNames(s) != "app,api,idle,web,notes" {
		t.Fatalf("order %q (%s)", projNames(s), s.State)
	}
	app, _ := projByName(s, "app")
	if app.State != "here" || !app.Attached || !app.Recent || app.Working != 1 || app.Waiting != 1 || app.OpenedAt != projT1.UnixMilli() ||
		app.Where != "~/src/app" || app.ID != webProjectID("/home/u/src/app") || len(app.ID) != webproto.ProjectIDLen {
		t.Errorf("app %+v", app)
	}
	// MOB-50: the row carries the workspace key the door routes by.
	if app.WS != session.Key("/home/u/src/app") || !webproto.ValidWS(app.WS) {
		t.Errorf("app ws %q", app.WS)
	}
	api, _ := projByName(s, "api")
	if api.State != "running" || !api.Service || !api.Recent || api.Working != 1 || api.Waiting != 1 || api.OpenedAt != projT2.UnixMilli() || api.Where != "~/src/api" {
		t.Errorf("api %+v", api)
	}
	if web, _ := projByName(s, "web"); web.State != "running" || web.Recent || web.Where != "web" || web.Attached {
		t.Errorf("web %+v", web)
	}
	if notes, _ := projByName(s, "notes"); notes.State != "asleep" || notes.Where != "notes" || !notes.Recent {
		t.Errorf("notes %+v", notes)
	}
	if s.Working != 2 || s.Waiting != 2 || s.More != 0 {
		t.Errorf("counts %+v", s)
	}
	b, _ := json.Marshal(s)
	for _, abs := range []string{`"/`, "/home/u", "/srv", "/opt", "relative"} {
		if strings.Contains(string(b), abs) {
			t.Errorf("%q goes out: %s", abs, b)
		}
	}
}

// Spec (MOB-8): at most 50 projects go out (this window's first); the
// rest are counted, and their agents still count.
func TestProjectsMergeCap(t *testing.T) {
	in := projectsInput{others: []agentroll.Rollup{{Workspace: "/home/u/p/old-00", PID: 3, Items: []agentroll.Item{{Name: "x", State: "waiting"}}}}}
	for i := range 60 {
		in.recent = append(in.recent, WorkspaceEntry{Path: fmt.Sprintf("/home/u/p/old-%02d", i), OpenedAt: projT0.Add(time.Duration(i) * time.Minute)})
	}
	s := projectsMerge("/home/u/p/here", nil, &in, "/home/u")
	if len(s.Projects) != webproto.MaxProjects || s.More != 11 || s.Projects[0].Name != "here" || s.Projects[1].Name != "old-00" || s.Projects[2].Name != "old-59" {
		t.Fatalf("%d projects, more %d, first %q", len(s.Projects), s.More, projNames(s)[:40])
	}
	if s.Waiting != 1 {
		t.Errorf("waiting %d", s.Waiting)
	}
	if loading := projectsMerge("/home/u/p/here", nil, nil, "/home/u"); loading.State != "loading" || projNames(loading) != "here" {
		t.Errorf("before the first read %+v", loading)
	}
}

// projTestRig is a Model in /home/u/src/app with a hub and a fake read.
func projTestRig(t *testing.T) (*Model, *projectsInput, *int) {
	t.Helper()
	t.Setenv("HOME", "/home/u")
	m := aiTestModel(t)
	m.ws = workspace.Workspace{ID: workspace.LocalID("/home/u/src/app"), Kind: workspace.KindLocal, Root: "/home/u/src/app"}
	webTestHub(t, &m)
	in := &projectsInput{
		recent:   []WorkspaceEntry{{Path: "/home/u/src/api", OpenedAt: projT2}, {Path: "/home/u/src/app", OpenedAt: projT1}, {Path: "/opt/notes", OpenedAt: projT0}},
		sessions: []session.Info{{Workspace: "/srv/web", PID: 7}},
		services: []string{"/home/u/src/api"},
		others:   []agentroll.Rollup{{Workspace: "/home/u/src/api", PID: 9, Items: []agentroll.Item{{Name: "codex", State: "working"}}}},
	}
	reads := 0
	old := webProjectsRead
	webProjectsRead = func() projectsInput { reads++; return *in }
	t.Cleanup(func() { webProjectsRead = old })
	return &m, in, &reads
}

func projTestSnaps(t *testing.T, frames []map[string]any) []webproto.ProjectsSnap {
	t.Helper()
	var out []webproto.ProjectsSnap
	for _, f := range frames {
		if f["topic"] == "projects" {
			var s webproto.ProjectsSnap
			webTestData(t, f, &s)
			out = append(out, s)
		}
	}
	return out
}

// Spec (MOB-8): a listening browser gets this window's project at once
// ("loading") and the laptop reads the files off the Update goroutine;
// the read sends one delta. A roll-up change shows on the next read;
// nothing changes, nothing goes out.
func TestProjectsTopicHub(t *testing.T) {
	m, in, reads := projTestRig(t)
	now := webTestClock(t)
	c := webTestSub(t, m, "projects")
	snaps := projTestSnaps(t, webTestNow(t, c))
	if len(snaps) != 1 || snaps[0].State != "loading" || projNames(snaps[0]) != "app" || snaps[0].Projects[0].State != "here" {
		t.Fatalf("first snap %+v", snaps)
	}
	cmd := m.webPublishProjects(nil)
	if cmd == nil || *reads != 0 {
		t.Fatalf("no load started (reads %d)", *reads)
	}
	if got := projTestSnaps(t, webTestNow(t, c)); len(got) != 0 {
		t.Fatalf("frames before the read: %+v", got)
	}
	if again := m.webPublishProjects(nil); again != nil {
		t.Fatal("a second load while one runs")
	}
	m.webPublishProjects(cmd())
	snaps = projTestSnaps(t, webTestNow(t, c))
	if len(snaps) != 1 || snaps[0].State != "ready" || projNames(snaps[0]) != "app,api,web,notes" || snaps[0].Working != 1 {
		t.Fatalf("after the read %+v", snaps)
	}
	if api, _ := projByName(snaps[0], "api"); api.Working != 1 || !api.Service {
		t.Errorf("api %+v", api)
	}

	in.others[0].Items = append(in.others[0].Items, agentroll.Item{Name: "claude", State: "waiting"})
	if cmd := m.webPublishProjects(nil); cmd != nil {
		t.Fatal("read again before webProjectsEvery")
	}
	*now = now.Add(webProjectsEvery)
	cmd = m.webPublishProjects(nil)
	if cmd == nil {
		t.Fatal("no read after webProjectsEvery")
	}
	m.webPublishProjects(cmd())
	snaps = projTestSnaps(t, webTestNow(t, c))
	if len(snaps) != 1 || snaps[0].Waiting != 1 || projNames(snaps[0]) != "app,api,web,notes" {
		t.Fatalf("after the roll-up change %+v", snaps)
	}
	*now = now.Add(webProjectsEvery)
	m.webPublishProjects(m.webPublishProjects(nil)())
	if got := projTestSnaps(t, webTestNow(t, c)); len(got) != 0 {
		t.Fatalf("frames without a change: %+v", got)
	}
}

// Spec (MOB-8, perf guard): with no browser on the topic nothing is
// read and nothing waits for a timer: no idle wakeups when nothing runs.
func TestProjectsNoReadWithoutListener(t *testing.T) {
	m, _, reads := projTestRig(t)
	webTestClock(t)
	for range 3 {
		if cmd := m.webPublishProjects(nil); cmd != nil {
			t.Fatal("a read without a listener")
		}
	}
	if *reads != 0 || m.web.projects.last != nil {
		t.Fatalf("reads %d, last %+v", *reads, m.web.projects.last)
	}
	c := webTestSub(t, m, "projects")
	m.webPublishProjects(m.webPublishProjects(nil)())
	webTestNow(t, c)
	if cmd := m.webPublishProjects(nil); cmd != nil {
		t.Fatal("an Update right after the read reads again")
	}
}

// Spec (MOB-8): the projects replay golden: the first read and a roll-up
// change replay to the final snapshot.
func TestProjectsReplayGolden(t *testing.T) {
	m, in, _ := projTestRig(t)
	now := webTestClock(t)
	h := m.web
	c := webReplayRecord(t, m, h, "projects", func() {
		m.webPublishProjects(m.webPublishProjects(nil)())
		in.others[0].Items = append(in.others[0].Items, agentroll.Item{Name: "claude", State: "waiting"})
		*now = now.Add(webProjectsEvery)
		m.webPublishProjects(m.webPublishProjects(nil)())
	})
	webReplayCheck(t, h.topics["projects"], c)
	webReplayGolden(t, webReplayPath("projects", "read_rollup"), c)
}
