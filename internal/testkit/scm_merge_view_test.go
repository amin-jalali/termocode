package testkit

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
)

// Source Control: the three-way merge view (G-MRG). Real nvim and git;
// skipped with -short.

// mergeDriver is scmNvimDriver at a chosen window size.
func mergeDriver(t *testing.T, w, h int, setup func(r *repo)) (*Driver, *repo) {
	t.Helper()
	if testing.Short() {
		t.Skip("starts nvim")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Cleanup(clock.Set(func() time.Time { return scmNow }))
	t.Setenv("TZ", "UTC")
	var r *repo
	d := New(t, Options{W: w, H: h, Nvim: true, Files: scmFiles, Setup: func(tb testing.TB, dir string) {
		r = newRepo(tb, dir)
		r.commit("init")
		setup(r)
	}})
	d.Action("view:git")
	if !d.WaitFor(func() bool { d.render(); return inSection(d, "CONFLICTS", "main.go") }, 5*time.Second) {
		t.Fatalf("no conflict listed:%s", dump(d))
	}
	return d, r
}

// clickConflict clicks main.go under CONFLICTS.
func clickConflict(d *Driver) {
	for y, l := range sidebar(d) {
		if strings.Contains(l, "main.go") {
			d.Click(colOf(d, y, "main.go"), y)
			return
		}
	}
	d.T.Fatalf("main.go not in the sidebar:%s", dump(d))
}

// Spec: Given a merge stopped on a conflict in main.go, when the user
// clicks main.go under CONFLICTS, then the merge view shows base, ours
// and theirs above the file, which is open in the editor with its
// markers; when the user presses Esc, then the view closes and the file
// stays open.
func TestSCMMergeViewOpensFromConflicts(t *testing.T) {
	d, _ := mergeDriver(t, 150, 42, conflictRepo)
	clickConflict(d)
	if !waitText(d, "Conflict 1 of 1") {
		t.Fatalf("merge view did not open:%s", dump(d))
	}
	for _, s := range []string{"Base", "Ours · HEAD", "Theirs · theirs", `println("ours")`, `println("theirs")`, "<<<<<<< HEAD"} {
		if !strings.Contains(text(d), s) {
			t.Errorf("screen misses %q:%s", s, dump(d))
		}
	}
	d.Key("esc")
	if !d.WaitFor(func() bool { d.render(); return !strings.Contains(text(d), "Conflict 1 of 1") }, 3*time.Second) {
		t.Fatalf("esc did not close the view:%s", dump(d))
	}
	if !strings.Contains(text(d), "<<<<<<< HEAD") {
		t.Errorf("file closed with the view:%s", dump(d))
	}
}
