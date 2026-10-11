package testkit

import (
	"strings"
	"testing"
	"time"
)

// Spec: Given the start page is showing (no file open) and Source Control
// has the keyboard focus, when the user presses down and then s, then the
// cursor moves in the changes list (not on the start page cards) and the
// first file is staged.
func TestSCMKeyboardNavigationOverStartPage(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n// edit\n")
	})
	d.WaitForText("main.go", 3*time.Second)
	d.Palette("git-focus")
	d.Key("down", "s")
	until(d, func() bool { return inSection(d, "STAGED", "main.go") })
	if got := r.porcelain(); got != "M  main.go" {
		t.Errorf("down, s: git status %q, want %q", got, "M  main.go")
	}
}

// Spec: Given the start page is showing and the commit box has the focus,
// when the user presses Enter, then Enter commits (it does not run the
// focused start-page card).
func TestSCMCommitBoxEnterOverStartPage(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n// edit\n")
		r.git("add", "main.go")
	})
	d.WaitForText("main.go", 3*time.Second)
	typeCommitMessage(d, "fix: enter commits")
	d.Key("enter")
	until(d, func() bool { return r.porcelain() == "" })
	if got := r.git("log", "-1", "--format=%s"); got != "fix: enter commits" {
		t.Errorf("Enter in the commit box did not commit: HEAD is %q", got)
	}
	if strings.Contains(text(d), "Open File") {
		t.Errorf("Enter opened a start-page action:%s", dump(d))
	}
}
