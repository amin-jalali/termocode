package app

import (
	"path/filepath"
	"testing"
	"time"
)

// fakeClock is a clock the test moves by hand.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

// sessionTestModel is a Model with a session writer on a fake clock, in
// a clean config dir and workspace.
func sessionTestModel(t *testing.T) (*Model, *fakeClock, string) {
	t.Helper()
	withConfigDir(t)
	dir := t.TempDir()
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	t.Chdir(dir)
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	m := &Model{}
	m.ensureSessionWriter().now = clk.now
	return m, clk, filepath.Join(dir, "a.go")
}

func scrollMsg(path string, line int) StateMsg {
	return StateMsg{
		Path:    path,
		Bufs:    []nvimBuffer{{Path: path}},
		Cursors: map[string][2]int{path: {line, 1}},
	}
}

func diskCursor(path string) [2]int { return loadSession().Cursors[path] }

// Scrolling for 20 s (a state message every 100 ms, the cursor moves each
// time) writes the session at most once per sessionMaxDelay, not 200
// times; the idle timer then writes the last position.
func TestSessionWriterCoalescesScroll(t *testing.T) {
	m, clk, path := sessionTestModel(t)
	var last func() any
	for i := 1; i <= 200; i++ {
		if cmd := m.persistStateSession(scrollMsg(path, i)); cmd != nil {
			last = func() any { return cmd() }
		}
		clk.t = clk.t.Add(100 * time.Millisecond)
	}
	if w := m.sess.writes; w > 15 {
		t.Fatalf("200 scroll state messages wrote the session %d times, want <= 15", w)
	}
	if got := diskCursor(path); got[0] == 200 {
		t.Fatalf("last position on disk before idle; the test does not prove the idle path")
	}
	if last == nil {
		t.Fatal("no idle timer for the last cursor move")
	}
	// The idle timer fires (wait for its message, as Bubble Tea would).
	msg, ok := last().(sessionIdleMsg)
	if !ok {
		t.Fatalf("idle command sent %T, want sessionIdleMsg", msg)
	}
	m.handleNvimMsg(msg)
	if got := diskCursor(path); got != [2]int{200, 1} {
		t.Errorf("after idle: cursor on disk %v, want [200 1]", got)
	}
}

// An idle timer from before the last change does not write: the editor
// is still busy, a newer timer is coming.
func TestSessionWriterStaleIdleWaits(t *testing.T) {
	m, clk, path := sessionTestModel(t)
	m.persistStateSession(scrollMsg(path, 1)) // first: open file, written at once
	clk.t = clk.t.Add(100 * time.Millisecond)
	m.persistStateSession(scrollMsg(path, 2))
	old := m.sess.gen
	clk.t = clk.t.Add(100 * time.Millisecond)
	m.persistStateSession(scrollMsg(path, 3))
	before := m.sess.writes
	m.handleNvimMsg(sessionIdleMsg{gen: old})
	if m.sess.writes != before {
		t.Errorf("stale idle timer wrote the session")
	}
	m.handleNvimMsg(sessionIdleMsg{gen: m.sess.gen})
	if got := diskCursor(path); got != [2]int{3, 1} {
		t.Errorf("cursor on disk %v, want [3 1]", got)
	}
}

// The same state again writes nothing; opening another file (a
// structural change) writes at once, without waiting for idle.
func TestSessionWriterSkipsUnchangedWritesStructural(t *testing.T) {
	m, clk, path := sessionTestModel(t)
	m.persistStateSession(scrollMsg(path, 5))
	n := m.sess.writes
	for i := 0; i < 10; i++ {
		clk.t = clk.t.Add(2 * time.Second)
		if cmd := m.persistStateSession(scrollMsg(path, 5)); cmd != nil {
			t.Fatal("unchanged state started an idle timer")
		}
	}
	if m.sess.writes != n {
		t.Fatalf("unchanged state wrote %d times", m.sess.writes-n)
	}
	other := filepath.Join(filepath.Dir(path), "b.go")
	msg := StateMsg{
		Path:    other,
		Bufs:    []nvimBuffer{{Path: path}, {Path: other}},
		Cursors: map[string][2]int{path: {5, 1}, other: {1, 1}},
	}
	m.persistStateSession(msg)
	s := loadSession()
	if s.ActivePath != other || len(s.OpenFiles) != 2 {
		t.Errorf("opened file not written at once: active %q files %v", s.ActivePath, s.OpenFiles)
	}
}

// Quit, Close (the testkit restart) and a hangup / crash (FlushSession)
// write the last cursor position that the idle timer has not written yet.
func TestSessionWriterFlushOnExit(t *testing.T) {
	for _, tc := range []struct {
		name string
		exit func(m *Model)
	}{
		{"quit", func(m *Model) { m.quitNow() }},
		{"close", func(m *Model) { _ = m.Close() }},
		{"hangup", func(*Model) { FlushSession() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, clk, path := sessionTestModel(t)
			m.persistStateSession(scrollMsg(path, 1))
			clk.t = clk.t.Add(100 * time.Millisecond)
			m.persistStateSession(scrollMsg(path, 42))
			if got := diskCursor(path); got[0] == 42 {
				t.Fatal("written before exit; the test does not prove the exit path")
			}
			tc.exit(m)
			if got := diskCursor(path); got != [2]int{42, 1} {
				t.Errorf("after %s: cursor on disk %v, want [42 1]", tc.name, got)
			}
		})
	}
}

// Other session fields written by their own helpers between two flushes
// survive the next flush.
func TestSessionWriterKeepsOtherFields(t *testing.T) {
	m, clk, path := sessionTestModel(t)
	m.persistStateSession(scrollMsg(path, 1))
	s := loadSession()
	s.TerminalRows = 21
	s.Roots = []string{"/extra"}
	saveSession(s)
	clk.t = clk.t.Add(100 * time.Millisecond)
	m.persistStateSession(scrollMsg(path, 9))
	m.sess.flush()
	got := loadSession()
	if got.TerminalRows != 21 || len(got.Roots) != 1 || got.Cursors[path] != [2]int{9, 1} {
		t.Errorf("after flush: rows %d roots %v cursor %v", got.TerminalRows, got.Roots, got.Cursors[path])
	}
}

// G-RST: the terminal tabs are written when they change, not on every
// state message, and only once the saved ones were opened at start.
func TestSessionWriterSavesTerminals(t *testing.T) {
	m, _, _ := sessionTestModel(t)
	m.termOpen = true
	m.terminalTabs = []terminalTab{{Name: "a", Cwd: "/a"}}
	m.observeTerminals()
	if loadSession().Terminals != nil {
		t.Fatal("before the start-up restore nothing may be written (it would drop the saved tabs)")
	}
	m.termRestoreReady = true
	m.observeTerminals()
	got := loadSession().Terminals
	if got == nil || len(got.Tabs) != 1 || got.Tabs[0].Panes[0].Cwd != "/a" || !got.Open {
		t.Fatalf("saved %+v", got)
	}
	writes := m.sess.writes
	m.observeTerminals()
	if m.sess.writes != writes {
		t.Error("unchanged terminals must not write again")
	}
	m.terminalTabs[0].Cwd = "/b" // shell integration: the shell moved
	m.observeTerminals()
	if got := loadSession().Terminals; got.Tabs[0].Panes[0].Cwd != "/b" {
		t.Fatalf("live folder not saved: %+v", got)
	}
	m.terminalTabs, m.termOpen = nil, false // the user closed the last shell
	m.observeTerminals()
	if got := loadSession().Terminals; got != nil {
		t.Fatalf("closed terminals must be forgotten: %+v", got)
	}
}

func TestSessionWriterTerminalRestoreOff(t *testing.T) {
	m, _, _ := sessionTestModel(t)
	m.termRestoreReady, m.termRestoreOff = true, true
	m.termOpen = true
	m.terminalTabs = []terminalTab{{Name: "a", Cwd: "/a"}}
	m.observeTerminals()
	if got := loadSession().Terminals; got != nil {
		t.Fatalf("terminal_restore off keeps nothing: %+v", got)
	}
}

// The editor's state messages and quitting hand the terminals to the writer.
func TestStateMsgAndQuitSaveTerminals(t *testing.T) {
	m, _, _ := sessionTestModel(t)
	m.termRestoreReady = true
	m.termOpen = true
	m.terminalTabs = []terminalTab{{Name: "a", Cwd: "/a"}}
	nm, _, _ := m.handleNvimMsg(StateMsg{})
	if got := loadSession().Terminals; got == nil || got.Tabs[0].Panes[0].Cwd != "/a" {
		t.Fatalf("state message: %+v", got)
	}
	nm.terminalTabs[0].Busy, nm.terminalTabs[0].LastCmd = true, "make watch"
	nm.quitNow()
	if got := loadSession().Terminals; got == nil || got.Tabs[0].Panes[0].Program != "make" {
		t.Fatalf("quit: %+v", got)
	}
}

// A click on an explorer row that leaves the expanded folders as they
// were writes nothing.
func TestSessionWriterExpandedUnchangedWritesNothing(t *testing.T) {
	m, _, _ := sessionTestModel(t)
	w := m.ensureSessionWriter()
	w.expandedChanged([]string{"/a", "/b"})
	n := w.writes
	w.expandedChanged([]string{"/a", "/b"})
	if w.writes != n {
		t.Fatalf("same folders wrote %d times", w.writes-n)
	}
	w.expandedChanged([]string{"/a"})
	if w.writes != n+1 {
		t.Fatalf("changed folders did not write (%d -> %d)", n, w.writes)
	}
}
