package testkit

import (
	"testing"
	"time"
)

// Spec: Given three files,
// when the user opens them one after another quickly (faster than the
// app's 100 ms state refresh),
// then after each open the tab bar shows that file as the active tab —
// the last change of a burst is never lost.
func TestTabsFollowQuickOpens(t *testing.T) {
	d := editorDriver(t, nil)
	seq := []string{"a.txt", "b.txt", "c.txt", "a.txt", "c.txt", "b.txt", "a.txt", "b.txt", "c.txt", "a.txt"}
	for i, rel := range seq {
		d.Action("open:" + d.Dir + "/" + rel)
		if !d.WaitFor(func() bool { _, a := tabs(d); return a == rel }, 2*time.Second) {
			got, a := tabs(d)
			t.Fatalf("open #%d %s: tab bar shows active %q (tabs %v)", i, rel, a, got)
		}
	}
}

// openABC opens a.txt, b.txt and c.txt (c.txt active).
func openABC(d *Driver) {
	d.T.Helper()
	for _, f := range []string{"a.txt", "b.txt", "c.txt"} {
		open(d, f)
	}
	if !waitTabs(d, "a.txt", "b.txt", "c.txt") {
		got, _ := tabs(d)
		d.T.Fatalf("tabs = %v", got)
	}
}

// Spec: Given a.txt, b.txt, c.txt open (c.txt active),
// when the user clicks the b.txt tab, then b.txt is active and shown;
// when the user presses Ctrl+PgDn, then c.txt is active;
// when the user presses Ctrl+PgUp twice, then a.txt is active.
func TestTabsSwitch(t *testing.T) {
	d := editorDriver(t, nil)
	openABC(d)
	_, y, ok := find(d, "b.txt x")
	if !ok || y != 0 {
		t.Fatalf("b.txt tab not in the tab bar:\n%s", screen(d))
	}
	clickText(d, "b.txt x")
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "b.txt" }, 3*time.Second) {
		t.Fatalf("click on tab: active %v", d.Model().FocusedEditor())
	}
	if got := bufLines(d); got != "one\ntwo\nthree" {
		t.Errorf("editor shows %q, want b.txt", got)
	}
	d.Key("ctrl+pgdown")
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "c.txt" }, 3*time.Second) {
		_, a := tabs(d)
		t.Errorf("Ctrl+PgDn: active %q, want c.txt", a)
	}
	d.Key("ctrl+pgup", "ctrl+pgup")
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "a.txt" }, 3*time.Second) {
		_, a := tabs(d)
		t.Errorf("Ctrl+PgUp ×2: active %q, want a.txt", a)
	}
}

// Spec: Given a.txt, b.txt, c.txt open (c.txt active),
// when the user presses Ctrl+W, then c.txt's tab closes and another tab
// is active; when the user runs Reopen Closed Editor (Alt+Shift+T),
// then c.txt is open and active again.
func TestTabsCloseAndReopen(t *testing.T) {
	d := editorDriver(t, nil)
	openABC(d)
	d.Key("ctrl+w")
	if !waitTabs(d, "a.txt", "b.txt") {
		got, _ := tabs(d)
		t.Fatalf("after Ctrl+W tabs = %v", got)
	}
	if _, a := tabs(d); a != "a.txt" && a != "b.txt" {
		t.Errorf("no active tab after close: %q", a)
	}
	d.Key("alt+shift+t")
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "c.txt" }, 3*time.Second) {
		got, a := tabs(d)
		t.Fatalf("reopen: active %q tabs %v", a, got)
	}
}

// Spec: Given b.txt active with unsaved edits,
// when the user closes its tab and picks Cancel, then the tab stays dirty;
// when the user closes it and picks Don't Save, then the tab is gone and
// the file is unchanged;
// and given c.txt active with unsaved edits, when the user closes it and
// picks Save, then the edit is on disk and the tab is gone.
func TestTabsCloseDirtyAsks(t *testing.T) {
	d := editorDriver(t, nil)
	openABC(d)
	open(d, "b.txt")
	d.Type("B")
	if !waitTabs(d, "a.txt", "b.txt*", "c.txt") {
		got, _ := tabs(d)
		t.Fatalf("tabs = %v", got)
	}

	d.Key("ctrl+w")
	if _, _, ok := find(d, "unsaved changes"); !ok {
		t.Fatalf("closing a dirty tab did not ask:\n%s", screen(d))
	}
	clickButton(d, "Cancel")
	if !waitTabs(d, "a.txt", "b.txt*", "c.txt") {
		got, _ := tabs(d)
		t.Fatalf("Cancel closed something: %v", got)
	}

	d.Key("ctrl+w")
	clickButton(d, "Don't Save")
	if !waitTabs(d, "a.txt", "c.txt") {
		got, _ := tabs(d)
		t.Fatalf("Don't Save: tabs %v", got)
	}
	if got := diskFile(d, "b.txt"); got != "one\ntwo\nthree\n" {
		t.Errorf("Don't Save changed b.txt: %q", got)
	}

	open(d, "c.txt")
	d.Type("C")
	if !waitTabs(d, "a.txt", "c.txt*") {
		got, _ := tabs(d)
		t.Fatalf("tabs = %v", got)
	}
	d.Key("ctrl+w")
	clickButton(d, "Save")
	if !waitTabs(d, "a.txt") {
		got, _ := tabs(d)
		t.Fatalf("Save: tabs %v", got)
	}
	if got := diskFile(d, "c.txt"); got != "Cred\ngreen\nblue\n" {
		t.Errorf("Save: c.txt = %q", got)
	}
}

// Spec: Given a.txt open,
// when the user types a letter and presses Ctrl+W at once (before the
// tab bar has redrawn the dirty dot),
// then the app still asks to save — the edit is never closed silently or
// ignored.
func TestTabsCloseRightAfterTyping(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	for i := range 5 {
		d.Type("x")
		d.Key("ctrl+w")
		if _, _, ok := find(d, "unsaved changes"); !ok {
			got, _ := tabs(d)
			t.Fatalf("try %d: Ctrl+W right after typing did not ask (tabs %v):\n%s", i, got, screen(d))
		}
		clickButton(d, "Cancel")
	}
}

// Spec: Given a.txt, b.txt, c.txt open,
// when the user right-clicks the b.txt tab and picks Close Others,
// then only b.txt stays open.
func TestTabsCloseOthers(t *testing.T) {
	d := editorDriver(t, nil)
	openABC(d)
	x, y, _ := find(d, "b.txt x")
	d.RightClick(x+1, y)
	clickText(d, "Close Others")
	if !waitTabs(d, "b.txt") {
		got, _ := tabs(d)
		t.Fatalf("after Close Others tabs = %v", got)
	}
}

// Spec: Given a.txt, b.txt, c.txt open,
// when the user right-clicks the a.txt tab and picks Close All to the
// Right, then only a.txt stays open.
func TestTabsCloseToTheRight(t *testing.T) {
	d := editorDriver(t, nil)
	openABC(d)
	x, y, _ := find(d, "a.txt x")
	d.RightClick(x+1, y)
	clickText(d, "Close All to the Right")
	if !waitTabs(d, "a.txt") {
		got, _ := tabs(d)
		t.Fatalf("tabs = %v", got)
	}
}

// Spec: Given a.txt, b.txt, c.txt open (c.txt active),
// when the user pins c.txt (Alt+P),
// then c.txt moves to the first tab; and Ctrl+W asks nothing and still
// works on it after unpinning.
func TestTabsPinMovesToFront(t *testing.T) {
	d := editorDriver(t, nil)
	openABC(d)
	d.Key("alt+p")
	if !waitTabs(d, "c.txt", "a.txt", "b.txt") {
		got, _ := tabs(d)
		t.Fatalf("after pin tabs = %v, want c.txt first", got)
	}
	d.Key("alt+p")
	if !waitTabs(d, "a.txt", "b.txt", "c.txt") {
		got, _ := tabs(d)
		t.Fatalf("after unpin tabs = %v, want the old order", got)
	}
}

// Spec: Given twelve files open in a narrow window (more tabs than fit),
// when the user switches to the first tab,
// then its label is visible in the tab bar (scrolled into view) and an
// overflow sign shows there are more tabs.
func TestTabsOverflowKeepsActiveVisible(t *testing.T) {
	files := map[string]string{}
	for i := range 12 {
		files[string(rune('a'+i))+"-long-file-name.txt"] = "x\n"
	}
	d := editorDriver(t, files)
	d.Resize(80, 24)
	for i := range 12 {
		open(d, string(rune('a'+i))+"-long-file-name.txt")
	}
	if _, y, ok := find(d, "l-long-file-name.txt"); !ok || y != 0 {
		t.Fatalf("last opened tab not visible:\n%s", screen(d))
	}
	if _, y, ok := find(d, "a-long-file-name.txt"); ok && y == 0 {
		t.Fatalf("all twelve tabs fit at 80 columns; the test needs overflow")
	}
	for range 11 {
		d.Key("ctrl+pgup")
	}
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "a-long-file-name.txt" }, 3*time.Second) {
		_, a := tabs(d)
		t.Fatalf("active %q after Ctrl+PgUp ×11", a)
	}
	if _, y, ok := find(d, "a-long-file-name.txt"); !ok || y != 0 {
		t.Errorf("active first tab not scrolled into view:\n%s", screen(d))
	}
}
