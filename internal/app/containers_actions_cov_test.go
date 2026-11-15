package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
)

// The container keys run the engine verb on the fake docker and clear
// the busy mark when the answer lands; a failure says so.
func TestCovContainerActionKeysRunTheVerb(t *testing.T) {
	m, f := ctrModel(t)
	for _, tc := range []struct{ id, key, verb string }{
		{"c3", "s", "start"}, {"c1", "x", "stop"}, {"c2", "r", "restart"}, {"c5", "p", "pause"}, {"c6", "p", "unpause"},
	} {
		cursorOnContainer(t, m, tc.id)
		cmd := ctrKey(m, tc.key)
		if cmd == nil || !m.services.busy[containerBusyPrefix+tc.id].Busy() {
			t.Fatalf("%s %s: no busy mark", tc.key, tc.id)
		}
		done := ctrFind[containerActionDoneMsg](t, ctrMsgs(cmd))
		if done.verb != tc.verb || done.id != tc.id || done.err != nil {
			t.Fatalf("%s: %+v", tc.key, done)
		}
		if f.Count(tc.verb+" "+tc.id) != 1 {
			t.Fatalf("fake docker calls %v", f.Calls())
		}
		_ = m.onContainerActionDone(done)
		if m.services.busy[containerBusyPrefix+tc.id].Busy() {
			t.Fatalf("%s: busy after done", tc.id)
		}
	}
	// Busy: a second press only says so.
	c := m.services.containerRows[0]
	m.services.busy[containerBusyPrefix+c.ID] = services.Starting
	if cmd := m.containerAction("stop", c); cmd == nil || !strings.Contains(ctrToast(m), "Container is busy") {
		t.Fatalf("busy toast: %s", ctrToast(m))
	}
	delete(m.services.busy, containerBusyPrefix+c.ID)
	if cmd := m.containerAction("dance", c); cmd != nil {
		t.Fatal("an unknown verb ran")
	}
	_ = m.onContainerActionDone(containerActionDoneMsg{id: c.ID, verb: "stop", err: errors.New("boom\nmore")})
	if !strings.Contains(ctrToast(m), "Could not stop container") {
		t.Fatalf("toast: %s", ctrToast(m))
	}
	m.services.containerEngine.Reachable = false
	_ = m.containerAction("stop", c)
	if !strings.Contains(ctrToast(m), "Container engine is not ready") {
		t.Fatalf("toast: %s", ctrToast(m))
	}
}

// s/x/r on a project group whose folder is gone run the verb on each of
// its containers (the fallback) and say so.
func TestCovContainerProjectGroupFallback(t *testing.T) {
	m, f := ctrModel(t)
	for _, c := range []struct{ key, verb, want string }{{"s", "up", "start"}, {"x", "stop", "stop"}, {"r", "restart", "restart"}} {
		ctrGroupRow(t, m, "shop")
		cmd := ctrKey(m, c.key)
		if cmd == nil || !m.services.busy[containerBusyPrefix+"c4"].Busy() {
			t.Fatalf("%s: no fallback", c.verb)
		}
		done := ctrFind[containerGroupDoneMsg](t, ctrMsgs(cmd))
		if !done.fallback || done.err != nil || f.Count(c.want+" c4") != 1 {
			t.Fatalf("%s: %+v calls %v", c.verb, done, f.Calls())
		}
		_ = m.onContainerGroupDone(done)
		if m.services.busy[containerBusyPrefix+"c4"].Busy() || !strings.Contains(ctrToast(m), "Project folder not available") {
			t.Fatalf("%s: toast %s", c.verb, ctrToast(m))
		}
	}
	// The engine fails: the done message carries the error.
	f.Set(t, "all.err", "daemon gone")
	ctrGroupRow(t, m, "shop")
	done := ctrFind[containerGroupDoneMsg](t, ctrMsgs(ctrKey(m, "x")))
	if done.err == nil {
		t.Fatal("no error")
	}
	_ = m.onContainerGroupDone(done)
	if !strings.Contains(ctrToast(m), "Project action failed") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onContainerGroupDone(containerGroupDoneMsg{project: "shop"})
	// A project with no containers at all.
	if cmd := m.projectGroupFallback("ghost", "up"); cmd == nil || !strings.Contains(ctrToast(m), "No containers were found for ghost") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// Not on a group row: nothing.
	cursorOnContainer(t, m, "c1")
	if m.projectGroupAction("up") != nil || m.askProjectDown() != nil {
		t.Fatal("ran off a group row")
	}
}

// With the project folder there, s on the group runs compose up -d in a
// terminal tab; without an editor the toast says it cannot.
func TestCovContainerProjectGroupCompose(t *testing.T) {
	m, _ := ctrModel(t)
	dir := t.TempDir()
	for i := range m.services.containerRows {
		if m.services.containerRows[i].Project == "shop" {
			m.services.containerRows[i].WorkingDir = dir
			m.services.containerRows[i].ConfigFiles = []string{filepath.Join(dir, "compose.yaml")}
		}
	}
	ctrGroupRow(t, m, "shop")
	_ = ctrKey(m, "s")
	if !strings.Contains(ctrToast(m), "Cannot up project") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if got := m.projectConfigFiles("shop"); len(got) != 1 {
		t.Fatalf("files %v", got)
	}
	ctrNvim(t, m)
	for _, c := range []struct {
		key   string
		state services.State
	}{{"s", services.Starting}, {"x", services.Stopping}, {"r", services.Restarting}} {
		ctrGroupRow(t, m, "shop")
		_ = ctrKey(m, c.key)
		if m.services.busy[containerBusyPrefix+"c4"] != c.state {
			t.Fatalf("%s: busy %v toast %s", c.key, m.services.busy, ctrToast(m))
		}
	}
	if !m.servicesFocusTab(containerTabPrefix + "compose:shop") {
		t.Fatal("no compose tab")
	}
	// The project of this folder uses the root when no container has a dir.
	m.services.project = "termocode"
	ctrGroupRow(t, m, "termocode")
	_ = m.askProjectDown()
	if !m.confirmOpen || m.confirmKind != confirmKindContainerProjectDown || m.pendingContainerDir != m.services.root {
		t.Fatalf("down card: open=%v dir=%q", m.confirmOpen, m.pendingContainerDir)
	}
}

// D on a group whose folder is missing never runs compose down.
func TestCovContainerProjectDownNeedsTheFolder(t *testing.T) {
	m, f := ctrModel(t)
	ctrGroupRow(t, m, "shop")
	_ = ctrKey(m, "D")
	if m.confirmOpen || !strings.Contains(ctrToast(m), "Cannot safely remove project") || f.Count("down") != 0 {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if projectDirExists("") || projectDirExists(filepath.Join(t.TempDir(), "nope")) {
		t.Fatal("missing dir exists")
	}
}

// Remove: the card's choice stops first for a running container, then
// removes it on the fake docker.
func TestCovContainerRemoveRuns(t *testing.T) {
	m, f := ctrModel(t)
	cursorOnContainer(t, m, "c1")
	_ = ctrKey(m, "D")
	if !m.confirmOpen || !m.pendingContainerStop {
		t.Fatal("no stop-remove card")
	}
	done := ctrFind[containerActionDoneMsg](t, ctrMsgs(m.runContainerRemove(true)))
	if done.err != nil || f.Count("stop c1") != 1 || f.Count("rm c1") != 1 || m.pendingContainerID != "" {
		t.Fatalf("%+v %v", done, f.Calls())
	}
	m.pendingContainerID = "c3"
	done = ctrFind[containerActionDoneMsg](t, ctrMsgs(m.runContainerRemove(false)))
	if done.err != nil || f.Count("stop c3") != 0 || f.Count("rm c3") != 1 {
		t.Fatalf("%+v %v", done, f.Calls())
	}
	f.Set(t, "all.err", "nope")
	m.pendingContainerID = "c1"
	done = ctrFind[containerActionDoneMsg](t, ctrMsgs(m.runContainerRemove(true)))
	if done.err == nil {
		t.Fatal("no error")
	}
}

// Enter/$/t/S open a terminal per kind; without an editor a toast says so,
// with one the tab opens once and the same key again only shows it.
func TestCovContainerTerminals(t *testing.T) {
	m, _ := ctrModel(t)
	c := m.services.containerRows[4]
	if m.openContainerTerminal(c, "dance") != nil {
		t.Fatal("unknown kind")
	}
	_ = m.openContainerTerminal(c, "logs")
	if !strings.Contains(ctrToast(m), "Cannot open logs") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	ctrNvim(t, m)
	cursorOnContainer(t, m, "c5")
	for _, k := range []string{"enter", "$", "S", "t"} {
		_ = ctrKey(m, k)
	}
	if !strings.Contains(ctrToast(m), "Attached to nginx") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	for _, kind := range []string{"logs", "shell", "stats", "attach"} {
		if !m.servicesFocusTab(containerTabPrefix + kind + ":c5") {
			t.Fatalf("no %s tab", kind)
		}
	}
	n := len(m.terminalTabs)
	if m.openContainerTerminal(c, "logs") != nil || len(m.terminalTabs) != n {
		t.Fatal("the second open made a new tab")
	}
}

// i inspects on the fake docker; the answer opens a page, an error toasts.
func TestCovContainerInspect(t *testing.T) {
	m, f := ctrModel(t)
	f.Set(t, "inspect.json", `[{"Id":"c5","Name":"/nginx"}]`)
	cursorOnContainer(t, m, "c5")
	msg := ctrFind[containerInspectMsg](t, ctrMsgs(ctrKey(m, "i")))
	if msg.err != nil || !strings.Contains(msg.text, `"Id": "c5"`) {
		t.Fatalf("%+v", msg)
	}
	if m.onContainerInspect(msg) != nil {
		t.Fatal("no editor: the page is skipped quietly")
	}
	f.Set(t, "inspect.json", `not json`)
	msg = ctrFind[containerInspectMsg](t, ctrMsgs(m.inspectContainer(m.services.containerRows[4])))
	if msg.err == nil {
		t.Fatal("bad json gave no error")
	}
	_ = m.onContainerInspect(msg)
	if !strings.Contains(ctrToast(m), "Cannot inspect container") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	ctrNvim(t, m)
	if cmd := m.onContainerInspect(containerInspectMsg{id: "gone", text: "{}"}); cmd != nil {
		t.Fatalf("page: %s", ctrToast(m))
	}
	if _, ok := m.containerByID("gone"); ok {
		t.Fatal("found a missing container")
	}
}

// o opens the first published port: local opens the URL, over SSH it
// copies it and shows the tunnel line, none says so.
func TestCovContainerOpenPort(t *testing.T) {
	m, _ := ctrModel(t)
	var opened []string
	old := osOpen
	osOpen = func(u string) error { opened = append(opened, u); return nil }
	t.Cleanup(func() { osOpen = old })
	var copied []string
	defer SetClipboardSink(func(s string) { copied = append(copied, s) })()
	c := containers.Container{ID: "p1", Names: []string{"web"}, Ports: []containers.Port{
		{PrivatePort: 80}, {IP: "::1", PrivatePort: 80, PublicPort: 8080},
	}}
	if m.containerOpenPort(c) != nil || len(opened) != 1 || opened[0] != "http://[::1]:8080" {
		t.Fatalf("opened %v", opened)
	}
	c.Ports = []containers.Port{{IP: "0.0.0.0", PublicPort: 9000}}
	_ = m.containerOpenPort(c)
	if opened[1] != "http://localhost:9000" {
		t.Fatalf("opened %v", opened)
	}
	osOpen = func(string) error { return errors.New("no browser") }
	_ = m.containerOpenPort(c)
	if !strings.Contains(ctrToast(m), "Cannot open port") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	t.Setenv("SSH_CONNECTION", "10.0.0.1 5000 10.0.0.2 22")
	_ = m.containerOpenPort(c)
	if len(copied) != 1 || copied[0] != "http://localhost:9000" || !strings.Contains(ctrToast(m), "ssh -L 9000:localhost:9000 10.0.0.2") {
		t.Fatalf("copied %v toast %s", copied, ctrToast(m))
	}
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "/dev/pts/1")
	_ = m.containerOpenPort(c)
	if !strings.Contains(ctrToast(m), "<host>") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.containerOpenPort(containers.Container{ID: "p2", Names: []string{"bare"}})
	if !strings.Contains(ctrToast(m), "bare has no published port") {
		t.Fatalf("toast %s", ctrToast(m))
	}
}
