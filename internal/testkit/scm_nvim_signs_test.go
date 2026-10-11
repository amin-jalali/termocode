package testkit

import (
	"strings"
	"testing"
	"time"
)

// Spec: Given main.go with a changed line and an added line (on disk, vs
// HEAD), when it is opened in the editor, then those lines get a ┃ sign in
// the gutter and an unchanged line gets none; when the change is committed
// from the commit box, then the signs go away.
func TestSCMNvimGutterSigns(t *testing.T) {
	d, r := scmNvimDriver(t, func(r *repo) {
		r.write("main.go", "package main\n\nfunc main() { println(1) }\n\n// added line\n")
	})
	d.Action("open:main.go")
	if !waitText(d, "added line") {
		t.Fatalf("main.go not opened:%s", dump(d))
	}
	d.WaitFor(func() bool { d.render(); return strings.Contains(signBefore(d, "// added line"), "┃") }, 5*time.Second)
	if s := signBefore(d, "func main() { println(1) }"); !strings.Contains(s, "┃") {
		t.Errorf("changed line has no sign: %q", s)
	}
	if s := signBefore(d, "// added line"); !strings.Contains(s, "┃") {
		t.Errorf("added line has no sign: %q", s)
	}
	if s := signBefore(d, "package main"); strings.Contains(s, "┃") {
		t.Errorf("unchanged line has a sign: %q", s)
	}

	d.Action("view:git")
	r.git("add", "main.go")
	d.Palette("git-refresh")
	typeCommitMessage(d, "feat: print")
	d.Key("enter")
	until(d, func() bool { return r.porcelain() == "" })
	if !d.WaitFor(func() bool { d.render(); return !strings.Contains(signBefore(d, "// added line"), "┃") }, 5*time.Second) {
		t.Errorf("signs still shown after the commit: %q", signBefore(d, "// added line"))
	}
}

// Spec: Given main.go opened at line 1 (committed by "init"), when the user
// runs Blame, then a toast names the commit, the author, the age and the
// subject; on a line not committed yet, it says "uncommitted".
func TestSCMNvimBlame(t *testing.T) {
	d, r := scmNvimDriver(t, func(r *repo) {
		r.write("main.go", "package main\n\nfunc main() {}\n// new\n")
	})
	short := r.git("rev-parse", "--short=7", "HEAD")
	d.Action("open:main.go")
	waitText(d, "// new")
	d.Palette("git-blame")
	if !waitText(d, short+" · Test · 3 days ago · init") {
		t.Errorf("blame toast for line 1 wrong:%s", dump(d))
	}
	if _, y := editorRow(d, "// new"); y >= 0 { // put the cursor on the uncommitted line
		d.Click(colOf(d, y, "// new")+1, y)
	}
	d.Settle(300 * time.Millisecond)
	d.Palette("git-blame")
	if !waitText(d, "uncommitted · you") {
		t.Errorf("blame toast for a new line wrong:%s", dump(d))
	}
}
