package testkit

import (
	"strings"
	"testing"
	"time"
)

// Source Control: blame in the editor (GITX-3, GITX-4).

// Spec: Given main.go committed by "init", when the user turns on the
// status-bar blame, the inline blame and the annotations, then the status
// bar names the author and age, the cursor line ends with author, age
// and subject, and the line starts with the commit's hash; a click on
// the status-bar blame opens the commit.
func TestSCMNvimBlameViews(t *testing.T) {
	d, r := scmNvimDriver(t, nil)
	short := r.git("rev-parse", "--short=7", "HEAD")
	d.Action("open:main.go")
	waitText(d, "package main")

	d.Palette("git-status-blame")
	if !waitText(d, "Test, 3 days ago") {
		t.Fatalf("no blame in the status bar:%s", dump(d))
	}
	d.Palette("git-inline-blame")
	if !d.WaitFor(func() bool { d.render(); return strings.Contains(lineWith(text(d), "package main"), "Test, 3 days ago · init") }, 5*time.Second) {
		t.Errorf("no inline blame on the cursor line:%s", dump(d))
	}
	d.Palette("git-blame-annotate")
	if !d.WaitFor(func() bool { d.render(); return strings.Contains(lineWith(text(d), "package main"), short+" Test") }, 5*time.Second) {
		t.Errorf("no annotation before the line:%s", dump(d))
	}
	d.Palette("git-blame-annotate")
	if !d.WaitFor(func() bool { d.render(); return !strings.Contains(lineWith(text(d), "package main"), short+" Test") }, 5*time.Second) {
		t.Errorf("annotation left after the toggle:%s", dump(d))
	}

	scr := strings.Split(text(d), "\n")
	last := scr[len(scr)-1]
	x := strings.Index(last, "Test, 3 days ago")
	if x < 0 {
		t.Fatalf("status bar blame gone:%q", last)
	}
	d.Click(len([]rune(last[:x]))+2, len(scr)-1)
	if !waitText(d, short+" · commit") {
		t.Errorf("the click did not open the commit:%s", dump(d))
	}
}
