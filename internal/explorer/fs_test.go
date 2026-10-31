package explorer

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"

	"github.com/amin-jalali/termocode/internal/containerhost"
	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
)

// fakeContainer is a container host on the fake engine with /app holding
// two folders and two files.
func fakeContainer(t *testing.T, noShell bool) (*containerhost.Host, *fakedocker.Docker, *fakedocker.Container) {
	t.Helper()
	d := fakedocker.New()
	c := d.Add(t, "api-1")
	c.Mkdir(t, "/app/zsub")
	c.Mkdir(t, "/app/asub")
	c.WriteFile(t, "/app/b.txt", []byte("b"), 0o644)
	c.WriteFile(t, "/app/asub/a.go", []byte("package a"), 0o644)
	c.WriteFile(t, "/app/main.go", []byte("package main"), 0o644)
	if noShell {
		c.NoShell()
	}
	h, err := containerhost.New(context.Background(), containerhost.Config{Container: "api-1", Root: "/app", X: d})
	if err != nil {
		t.Fatal(err)
	}
	return h, d, c
}

// drainLoads runs the asked container listings and applies them, until
// none is left (what the app's Update loop does with ScheduleLoads).
func drainLoads(t *testing.T, m *Model) {
	t.Helper()
	for i := 0; i < 100; i++ {
		cmd := m.ScheduleLoads()
		if cmd == nil {
			return
		}
		for _, msg := range runCmds(cmd) {
			m.ApplyDirLoaded(msg.(DirLoadedMsg))
		}
	}
	t.Fatal("listings never ended")
}

// runCmds runs cmd and every command of a batch it returns.
func runCmds(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, runCmds(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func childNames(n *Node) []string {
	var out []string
	for _, c := range n.Children {
		out = append(out, c.Name)
	}
	return out
}

func TestNewRootFSListsTheContainer(t *testing.T) {
	for _, noShell := range []bool{false, true} {
		h, d, _ := fakeContainer(t, noShell)
		before := len(d.Calls())
		m := NewFS(h.FS(), "/app")
		if n := len(d.Calls()) - before; n != 0 {
			t.Fatalf("NewFS made %d engine calls on the UI loop, want 0", n)
		}
		drainLoads(t, &m)
		root := m.root
		if !root.IsDir || !root.Expanded || root.Path != "/app" || root.Name != "app" {
			t.Fatalf("root = %+v", root)
		}
		got := childNames(root)
		want := []string{"asub", "zsub", "b.txt", "main.go"}
		if len(got) != len(want) {
			t.Fatalf("noShell %v: children = %v, want %v", noShell, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("noShell %v: children = %v, want %v", noShell, got, want)
			}
		}
		if root.Children[0].Path != "/app/asub" || !root.Children[0].IsDir {
			t.Fatalf("asub = %+v", root.Children[0])
		}
		// One listing: never one call per entry (without sh each call
		// reads a whole tar).
		if n := len(d.Calls()) - before; n > 2 {
			t.Fatalf("noShell %v: %d engine calls for one folder, want <= 2", noShell, n)
		}
		// Children list through the same FS.
		if err := root.Children[0].Toggle(); err != nil {
			t.Fatal(err)
		}
		drainLoads(t, &m)
		if got := childNames(root.Children[0]); len(got) != 1 || got[0] != "a.go" || root.Children[0].Children[0].Path != "/app/asub/a.go" {
			t.Fatalf("asub children = %v", got)
		}
	}
}

func TestNewFSReloadStaysInTheContainer(t *testing.T) {
	h, _, c := fakeContainer(t, false)
	m := NewFS(h.FS(), "/app")
	drainLoads(t, &m)
	if m.err != "" || m.PrimaryRoot() != "/app" {
		t.Fatalf("model err %q root %q", m.err, m.PrimaryRoot())
	}
	m.ExpandPaths([]string{"/app/asub"})
	drainLoads(t, &m)
	c.WriteFile(t, "/app/new.txt", []byte(""), 0o644)
	c.WriteFile(t, "/app/asub/b.go", []byte(""), 0o644)
	m.Reload()
	drainLoads(t, &m)
	if m.PrimaryRoot() != "/app" {
		t.Fatalf("Reload moved the root to %q", m.PrimaryRoot())
	}
	if got := childNames(m.root); len(got) != 5 || got[4] != "new.txt" {
		t.Fatalf("root after reload = %v", got)
	}
	asub := m.root.Children[0]
	if !asub.Expanded || len(asub.Children) != 2 || asub.FS() == nil {
		t.Fatalf("asub after reload = %+v", asub)
	}
}

func TestNewFSShowsAnError(t *testing.T) {
	h, _, c := fakeContainer(t, false)
	c.Stop()
	m := NewFS(h.FS(), "/app")
	drainLoads(t, &m)
	if m.err == "" {
		t.Fatal("no error for a stopped container")
	}
}

func TestPreviewReadsTheContainerFile(t *testing.T) {
	h, _, c := fakeContainer(t, false)
	c.WriteFile(t, "/app/big.log", make([]byte, 2<<20), 0o644)
	m := NewFS(h.FS(), "/app")
	drainLoads(t, &m)
	m.SetSize(76, 30)
	m.SetTerminalWidth(150)
	t.Cleanup(clock.SetAfter(func(time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		ch <- clock.Now()
		return ch
	}))
	for path, want := range map[string]string{
		"/app/main.go": "package main",
		"/app/asub":    "1 files, 0 folders",
		"/app/big.log": "Large file",
	} {
		m.previewOn = true
		m.RevealPath(path)
		drainLoads(t, &m)
		cmd := m.SchedulePreview()
		if cmd == nil {
			t.Fatalf("%s: no preview", path)
		}
		msg := cmd().(PreviewMsg)
		if !strings.Contains(msg.Text, want) {
			t.Fatalf("%s: preview = %q, want %q", path, msg.Text, want)
		}
	}
}

// FIX-2: a hung engine does not freeze the UI. Opening a container
// folder, a reload and a reveal make no engine call in place: the folder
// shows as loading, and its listing runs as a command and fills it when
// it comes back. A session restore and a reveal go on when the folders
// they need arrive; a failed root listing is the explorer's error.
func TestContainerListingsRunOffTheUILoop(t *testing.T) {
	h, d, c := fakeContainer(t, false)
	c.Mkdir(t, "/app/asub/deep")
	c.WriteFile(t, "/app/asub/deep/x.go", []byte("package x"), 0o644)
	m := NewFS(h.FS(), "/app")
	m.SetSize(40, 20)
	if !m.root.loading || !strings.Contains(m.View(true, "", false, nil), "app …") {
		t.Fatalf("the root does not show as loading:\n%s", m.View(true, "", false, nil))
	}
	drainLoads(t, &m)

	calls := len(d.Calls())
	asub := m.root.Children[0]
	if err := asub.Toggle(); err != nil {
		t.Fatal(err)
	}
	m.Reload()
	m.RevealPath("/app/asub/deep/x.go")
	if n := len(d.Calls()) - calls; n != 0 {
		t.Fatalf("%d engine calls on the UI loop, want 0", n)
	}
	if !asub.loading || !asub.Expanded {
		t.Fatalf("asub: loading %v expanded %v", asub.loading, asub.Expanded)
	}
	drainLoads(t, &m)
	if got := m.CursorPath(); got != "/app/asub/deep/x.go" {
		t.Errorf("the reveal did not go on: cursor %q", got)
	}
	if asub.loading || len(asub.Children) != 2 {
		t.Errorf("asub after its listing: loading %v children %v", asub.loading, childNames(asub))
	}

	// A session restore of a folder two levels down.
	m2 := NewFS(h.FS(), "/app")
	m2.ExpandPaths([]string{"/app/asub", "/app/asub/deep"})
	drainLoads(t, &m2)
	if n := findNode(m2.root, "/app/asub/deep"); n == nil || !n.Expanded || len(n.Children) != 1 {
		t.Errorf("restore: deep = %+v", n)
	}

	// The engine stops: the root's listing is the explorer's error.
	c.Stop()
	m.Reload()
	drainLoads(t, &m)
	if m.err == "" {
		t.Error("no error after the container stopped")
	}
	c.Start()
	m.Reload()
	drainLoads(t, &m)
	if m.err != "" {
		t.Errorf("the error stays after the container came back: %q", m.err)
	}
}

// A listing of a workspace that was replaced, or of a folder no longer
// in the tree, is dropped.
func TestStaleContainerListingIsDropped(t *testing.T) {
	h, _, _ := fakeContainer(t, false)
	m := NewFS(h.FS(), "/app")
	cmd := m.ScheduleLoads()
	m2 := NewFS(h.FS(), "/app")
	for _, msg := range runCmds(cmd) {
		m2.ApplyDirLoaded(msg.(DirLoadedMsg))
	}
	if len(m2.root.Children) != 0 || !m2.root.loading {
		t.Fatalf("another root's listing filled this one: %v", childNames(m2.root))
	}
	m2.ApplyDirLoaded(DirLoadedMsg{Path: "/app/gone", ld: m2.root.ld})
}
