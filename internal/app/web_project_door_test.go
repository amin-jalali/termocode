package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// doorFake is the cmd/termocode side of the door for a test.
type doorFake struct {
	running map[string]bool
	service map[string]bool
	started []string
	idle    []int
	err     error
}

func doorRig(t *testing.T, known ...string) (*webFilesRig, *doorFake) {
	t.Helper()
	r, _ := webWorkspaceRig(t)
	if r.m.output == nil {
		r.m.output = newOutputStore() // the audit lines
	}
	f := &doorFake{running: map[string]bool{}, service: map[string]bool{}}
	oldPS, oldRead, oldDoor, oldSelf := projectSessions, webProjectsRead, webDoor, webDoorSelf
	t.Cleanup(func() {
		projectSessions, webProjectsRead, webDoor, webDoorSelf = oldPS, oldRead, oldDoor, oldSelf
	})
	webDoor = webDoorState{opening: map[string]string{}, opened: map[string]string{}}
	webDoorSelf = webDoorSelfState{}
	projectSessions = ProjectSessions{
		Running: func(dir string) bool { return f.running[dir] },
		Start: func(dir string, idle int) error {
			f.started, f.idle = append(f.started, dir), append(f.idle, idle)
			if f.err == nil {
				f.running[dir] = true
			}
			return f.err
		},
		Service: func(dir string) bool { return f.service[dir] },
	}
	in := projectsInput{}
	for _, k := range known {
		in.recent = append(in.recent, WorkspaceEntry{Path: k, OpenedAt: time.Now()})
	}
	webProjectsRead = func() projectsInput { return in }
	return r, f
}

// Spec (MOB-51): project.open names a known project by its ws and waits
// for the laptop: the card (Cancel first) names the folder and the
// device; nothing starts before the yes. On yes the session starts
// headless with web_project_idle_minutes, the door remembers it, and the
// audit log and a toast say so.
func TestWebProjectOpenAsksTheLaptopThenStartsHeadless(t *testing.T) {
	shop := t.TempDir()
	r, f := doorRig(t, shop)
	var ref webproto.AskRef
	webTestData(t, r.call(webproto.OpProjectOpen, &webproto.ProjectArgs{WS: session.Key(shop)}), &ref)
	if ref.Card == "" || len(f.started) != 0 {
		t.Fatalf("card %q, started before the yes: %v", ref.Card, f.started)
	}
	v := r.m.confirm.View()
	for _, want := range []string{"Open " + filepath.Base(shop) + " for the phone?", "Cancel", "60 minutes"} {
		if !strings.Contains(v, want) {
			t.Errorf("card misses %q:\n%s", want, v)
		}
	}
	webTestLaptopSelect(t, r.m, "confirm")
	if len(f.started) != 1 || f.started[0] != shop || f.idle[0] != 60 {
		t.Fatalf("started %v idle %v", f.started, f.idle)
	}
	if webDoor.opened[session.Key(shop)] != shop || len(webDoor.opening) != 0 {
		t.Fatalf("door %+v", webDoor)
	}
	if out := webTestAudit(r.m); !strings.Contains(out, "project.open") || !strings.Contains(out, "opened headless") {
		t.Errorf("audit:\n%s", out)
	}
}

// Spec (MOB-51): the laptop's Cancel starts nothing.
func TestWebProjectOpenCancelStartsNothing(t *testing.T) {
	shop := t.TempDir()
	r, f := doorRig(t, shop)
	webTestData(t, r.call(webproto.OpProjectOpen, &webproto.ProjectArgs{WS: session.Key(shop)}), &webproto.AskRef{})
	webTestLaptopSelect(t, r.m, "cancel")
	if len(f.started) != 0 || len(webDoor.opened) != 0 {
		t.Fatalf("started %v", f.started)
	}
}

// Spec (MOB-51): what project.open refuses without a card: an unknown
// id (folders are added on the laptop only), this project itself, a
// project that runs already (the door joins it), a folder that is gone,
// more than web_projects_max projects the door started, a viewer, and a
// system with no sessions.
func TestWebProjectOpenRefusals(t *testing.T) {
	shop, blog, gone := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "gone")
	r, f := doorRig(t, shop, blog, gone)
	writeUserConfig(t, `{"web_projects_max":1}`)
	open := func(ws string) map[string]any {
		return r.call(webproto.OpProjectOpen, &webproto.ProjectArgs{WS: ws})
	}
	if c := webErrCode(open("0123456789abcdef")); c != "not_found" {
		t.Errorf("unknown: %s", c)
	}
	if c := webErrCode(open(webDoorHome())); c != "conflict" {
		t.Errorf("home: %s", c)
	}
	if c := webErrCode(open(session.Key(gone))); c != "gone" {
		t.Errorf("gone folder: %s", c)
	}
	f.running[shop] = true
	if c := webErrCode(open(session.Key(shop))); c != "conflict" {
		t.Errorf("running: %s", c)
	}
	webDoor.opened[session.Key(shop)] = shop // the door started shop: 1 of 1
	res := open(session.Key(blog))
	if c := webErrCode(res); c != "limit" || !strings.Contains(webTestMsg(res), "1 of 1") {
		t.Errorf("cap: %v", res)
	}
	if webproto.Allowed(webproto.RoleViewer, webproto.OpProjectOpen) { // the conn refuses a viewer before the hub (web_call.go)
		t.Error("a viewer may open a project")
	}
	projectSessions = ProjectSessions{}
	if c := webErrCode(open(session.Key(blog))); c != "forbidden" {
		t.Errorf("no sessions here: %s", c)
	}
	if r.m.confirmOpen || len(f.started) != 0 {
		t.Fatalf("a refusal opened a card or started: %v", f.started)
	}
}

// webTestMsg is an err frame's text.
func webTestMsg(f map[string]any) string { s, _ := f["msg"].(string); return s }

// Spec (MOB-51): a service-installed folder starts with no idle stop (its
// service runs it; the door never stops it), and a failed start is a
// toast, an audit line and nothing remembered.
func TestWebProjectOpenServiceAndFailure(t *testing.T) {
	shop, blog := t.TempDir(), t.TempDir()
	r, f := doorRig(t, shop, blog)
	f.service[shop] = true
	webTestData(t, r.call(webproto.OpProjectOpen, &webproto.ProjectArgs{WS: session.Key(shop)}), &webproto.AskRef{})
	webTestLaptopSelect(t, r.m, "confirm")
	if len(f.idle) != 1 || f.idle[0] != 0 {
		t.Fatalf("service folder idle %v, want 0", f.idle)
	}
	f.err = errors.New("the project's web hub did not start")
	webTestData(t, r.call(webproto.OpProjectOpen, &webproto.ProjectArgs{WS: session.Key(blog)}), &webproto.AskRef{})
	webTestLaptopSelect(t, r.m, "confirm")
	if _, ok := webDoor.opened[session.Key(blog)]; ok {
		t.Fatal("a failed start was remembered")
	}
	if out := webTestAudit(r.m); !strings.Contains(out, "failed") {
		t.Errorf("audit:\n%s", out)
	}
}

// Spec (MOB-51): while the door opens a project, the projects topic
// shows it "opening"; this window stays "here".
func TestWebProjectsTopicShowsOpening(t *testing.T) {
	shop := t.TempDir()
	r, _ := doorRig(t, shop)
	in := webProjectsRead()
	r.m.web.projects.in = &in
	webDoor.opening[session.Key(shop)] = shop
	webDoor.opening[webDoorHome()] = r.root
	snap := r.m.webProjectsBuild()
	got := map[string]string{}
	for _, p := range snap.Projects {
		got[p.WS] = p.State
	}
	if got[session.Key(shop)] != webproto.ProjectOpening || got[webDoorHome()] != "here" {
		t.Fatalf("states %v", got)
	}
}

// Spec (MOB-51): a UI the door started takes the idle minutes from its
// start env once and removes the variable (nothing it starts inherits
// it); a value outside 5..240 is ignored.
func TestWebDoorSelfReadsItsEnv(t *testing.T) {
	now := time.Unix(1000, 0)
	for v, want := range map[string]time.Duration{"60": time.Hour, "4": 0, "241": 0, "x": 0, "": 0} {
		var s webDoorSelfState
		unset := ""
		s.read(func(k string) string {
			if k == session.EnvDoorIdle {
				return v
			}
			return ""
		}, func(k string) error { unset = k; return nil }, now)
		if s.idle != want {
			t.Errorf("%q: idle %v want %v", v, s.idle, want)
		}
		if v != "" && unset != session.EnvDoorIdle {
			t.Errorf("%q: env not removed", v)
		}
	}
}

// doorSelfRig is this UI as a project the door started 60 idle minutes
// ago at most.
func doorSelfRig(t *testing.T) (*webFilesRig, *int, *time.Time) {
	t.Helper()
	r, _ := doorRig(t)
	quits := 0
	oldQuit, oldAtt, oldNow := webDoorQuit, webDoorAttached, webNow
	t.Cleanup(func() { webDoorQuit, webDoorAttached, webNow = oldQuit, oldAtt, oldNow })
	webDoorQuit = func() { quits++ }
	webDoorAttached = func() bool { return false }
	now := time.Unix(100000, 0)
	webNow = func() time.Time { return now }
	webDoorSelf = webDoorSelfState{idle: time.Hour, last: now}
	return r, &quits, &now
}

// Spec (MOB-51): a door-started project stops by itself after the idle
// time with no use; never while a card waits (busy counts as use) and
// never once someone attached to it on the laptop.
func TestWebDoorIdleStop(t *testing.T) {
	r, quits, now := doorSelfRig(t)
	*now = now.Add(59 * time.Minute)
	if r.m.webDoorIdleTick() == nil || *quits != 0 {
		t.Fatalf("stopped early: %d", *quits)
	}
	r.m.confirmOpen = true // a card waits
	*now = now.Add(2 * time.Hour)
	if r.m.webDoorIdleTick() == nil || *quits != 0 {
		t.Fatal("stopped while a card waits")
	}
	r.m.confirmOpen = false
	*now = now.Add(61 * time.Minute)
	r.m.webDoorIdleTick()
	if *quits != 1 {
		t.Fatalf("not stopped after the idle time: %d", *quits)
	}
	if out := webTestAudit(r.m); !strings.Contains(out, "idle minutes") {
		t.Errorf("audit:\n%s", out)
	}

	webDoorSelf = webDoorSelfState{idle: time.Hour, last: *now}
	webDoorAttached = func() bool { return true }
	*now = now.Add(3 * time.Hour)
	if r.m.webDoorIdleTick() != nil || *quits != 1 || !webDoorSelf.laptop {
		t.Fatalf("an attached project: quits %d self %+v", *quits, webDoorSelf)
	}
}

// Spec (MOB-51): a web call is use: it moves the idle time on.
func TestWebDoorCallIsUse(t *testing.T) {
	r, _, now := doorSelfRig(t)
	*now = now.Add(50 * time.Minute)
	r.m.webDoorAfter(webCallMsg{})
	if !webDoorSelf.last.Equal(*now) {
		t.Fatalf("last %v want %v", webDoorSelf.last, *now)
	}
}

// Spec (MOB-51): project.close on the project's own hub stops it after
// the answer; it refuses another project's id, a project opened on the
// laptop (no idle env, or attached), and a busy one.
func TestWebProjectClose(t *testing.T) {
	r, quits, _ := doorSelfRig(t)
	cl := func(ws string) map[string]any {
		return r.call(webproto.OpProjectClose, &webproto.ProjectArgs{WS: ws})
	}
	if c := webErrCode(cl("0123456789abcdef")); c != "not_found" {
		t.Errorf("other id: %s", c)
	}
	r.m.confirmOpen = true
	if c := webErrCode(cl(webDoorHome())); c != "conflict" {
		t.Errorf("busy: %s", c)
	}
	r.m.confirmOpen = false
	webDoorAttached = func() bool { return true }
	if c := webErrCode(cl(webDoorHome())); c != "forbidden" {
		t.Errorf("attached: %s", c)
	}
	webDoorAttached = func() bool { return false }
	var done webproto.ProjectClosed
	webResData(t, cl(webDoorHome()), &done)
	if done.WS != webDoorHome() {
		t.Fatalf("closed %+v", done)
	}
	webDoorSelf = webDoorSelfState{}
	if c := webErrCode(cl(webDoorHome())); c != "forbidden" {
		t.Errorf("opened on the laptop: %s", c)
	}
	_ = quits
}

// Spec (MOB-51): the AI can never open or close a project: no AI or MCP
// source names the ops or the door's start.
func TestAICannotOpenProjects(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		base := filepath.Base(f)
		if strings.HasSuffix(base, "_test.go") || !(strings.HasPrefix(base, "ai_") || strings.HasPrefix(base, "mcp_")) {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"OpProjectOpen", "OpProjectClose", "webDoorStart", "webProjectOpen", "projectSessions"} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s names %s", base, bad)
			}
		}
	}
}
