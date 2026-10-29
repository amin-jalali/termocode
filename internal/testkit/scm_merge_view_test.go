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

// twoConflictRepo leaves a merge stopped on two conflicts in main.go.
func twoConflictRepo(r *repo) {
	body := func(a, b string) string {
		return "package main\n\nfunc a() { println(\"" + a + "\") }\n\n// 1\n// 2\n// 3\n// 4\n// 5\n// 6\n\nfunc b() { println(\"" + b + "\") }\n"
	}
	r.write("main.go", body("a", "b"))
	r.commit("base")
	r.git("checkout", "-q", "-b", "theirs")
	r.write("main.go", body("a-theirs", "b-theirs"))
	r.commit("feat: theirs")
	r.git("checkout", "-q", "main")
	r.write("main.go", body("a-ours", "b-ours"))
	r.commit("feat: ours")
	if _, err := r.try("merge", "--no-edit", "theirs"); err == nil {
		r.t.Fatal("setup: merge did not conflict")
	}
}

// waitMerge waits for the merge view's header to say s.
func waitMerge(d *Driver, s string) {
	d.T.Helper()
	if !d.WaitFor(func() bool { d.render(); return strings.Contains(text(d), s) }, 5*time.Second) {
		d.T.Fatalf("merge header never said %q:%s", s, dump(d))
	}
}

// Spec: Given a merge view on a file with two conflicts, when the user
// takes theirs and then ours, then no conflicts are left; each undo (u)
// brings back exactly one block; then Take Base and Take Both (theirs
// first) resolve it again, and saving writes that result.
func TestSCMMergeViewTakesAndUndo(t *testing.T) {
	d, r := mergeDriver(t, 150, 42, twoConflictRepo)
	clickConflict(d)
	waitMerge(d, "Conflict 1 of 2")
	d.Key("t")
	waitMerge(d, "Conflict 1 of 1")
	d.Key("o")
	waitMerge(d, "No conflicts left")
	d.Key("u")
	waitMerge(d, "Conflict 1 of 1")
	d.Key("u")
	waitMerge(d, "Conflict 1 of 2")
	d.Key("a")
	waitMerge(d, "Conflict 1 of 1")
	d.Key("B")
	waitMerge(d, "No conflicts left")
	d.Key("ctrl+s")
	want := "package main\n\nfunc a() { println(\"a\") }\n\n// 1\n// 2\n// 3\n// 4\n// 5\n// 6\n\nfunc b() { println(\"b-theirs\") }\nfunc b() { println(\"b-ours\") }\n"
	if !d.WaitFor(func() bool { return r.read("main.go") == want }, 5*time.Second) {
		t.Fatalf("main.go = %q, want %q", r.read("main.go"), want)
	}
}

// Spec: Given the merge view on a conflict, when the user presses r with
// markers left, then it says so and nothing is staged; when the user
// takes theirs and presses r, then the file is saved and staged, the view
// closes and CONFLICTS goes away.
func TestSCMMergeViewMarkResolved(t *testing.T) {
	d, r := mergeDriver(t, 150, 42, conflictRepo)
	clickConflict(d)
	waitMerge(d, "Conflict 1 of 1")
	d.Key("r")
	waitMerge(d, "Can't mark as resolved")
	if got := r.porcelain(); !strings.Contains(got, "UU main.go") {
		t.Fatalf("staged with markers: %q", got)
	}
	d.Key("t")
	waitMerge(d, "No conflicts left")
	d.Key("r")
	waitMerge(d, "Marked as resolved: main.go")
	if !until(d, func() bool { return section(d, "CONFLICTS") == nil }) {
		t.Fatalf("CONFLICTS still shown:%s", dump(d))
	}
	if strings.Contains(text(d), "No conflicts left") {
		t.Errorf("merge view still open:%s", dump(d))
	}
	// r.git trims the output.
	if got := r.git("show", ":main.go"); got != "package main\n\nfunc main() { println(\"theirs\") }" {
		t.Errorf("staged main.go = %q", got)
	}
}

// rebaseConflictRepo leaves a rebase of "feature" onto main stopped on a
// conflict in main.go.
func rebaseConflictRepo(r *repo) {
	r.git("checkout", "-q", "-b", "feature")
	r.write("main.go", "package main\n\nfunc main() { println(\"feature\") }\n")
	r.commit("feat: feature")
	r.git("checkout", "-q", "main")
	r.write("main.go", "package main\n\nfunc main() { println(\"main\") }\n")
	r.commit("feat: main")
	r.git("checkout", "-q", "feature")
	if _, err := r.try("rebase", "main"); err == nil {
		r.t.Fatal("setup: rebase did not conflict")
	}
}

// Spec: Given a rebase stopped on a conflict, when the user resolves it in
// the merge view, then the file is staged and the app says to continue
// the rebase from Source Control.
func TestSCMMergeViewRebase(t *testing.T) {
	d, r := mergeDriver(t, 150, 42, rebaseConflictRepo)
	clickConflict(d)
	waitMerge(d, "Conflict 1 of 1")
	d.Key("b")
	waitMerge(d, "No conflicts left")
	d.Key("r")
	waitMerge(d, "Continue the rebase")
	want := "package main\n\nfunc main() { println(\"main\") }\nfunc main() { println(\"feature\") }"
	if got := r.git("show", ":main.go"); got != want { // r.git trims
		t.Errorf("staged main.go = %q, want %q", got, want)
	}
}

// deleteConflictRepo leaves a merge stopped on main.go changed by ours
// and deleted by theirs.
func deleteConflictRepo(r *repo) {
	r.git("checkout", "-q", "-b", "theirs")
	r.git("rm", "-q", "main.go")
	r.commit("feat: delete main.go")
	r.git("checkout", "-q", "main")
	r.write("main.go", "package main\n\nfunc main() { println(\"ours\") }\n")
	r.commit("feat: ours")
	if _, err := r.try("merge", "--no-edit", "theirs"); err == nil {
		r.t.Fatal("setup: merge did not conflict")
	}
}

// Spec: Given main.go changed by ours and deleted by theirs, when the user
// opens it from CONFLICTS, then a card asks Keep ours / Keep theirs /
// Delete with Cancel focused; Enter changes nothing; picking Keep ours
// keeps the file and stages it.
func TestSCMMergeViewDeletedCard(t *testing.T) {
	d, r := mergeDriver(t, 150, 42, deleteConflictRepo)
	clickConflict(d)
	waitMerge(d, "Keep theirs")
	if !strings.Contains(text(d), "Theirs deleted it") {
		t.Errorf("card does not say theirs deleted it:%s", dump(d))
	}
	d.Key("enter")
	if got := r.porcelain(); !strings.Contains(got, "UD main.go") {
		t.Fatalf("enter changed the repo: %q", got)
	}
	clickConflict(d)
	waitMerge(d, "Keep theirs")
	d.Key("right")
	d.Key("enter")
	waitMerge(d, "Kept ours: main.go")
	if !d.WaitFor(func() bool { return strings.Contains(r.porcelain(), "M  main.go") || r.porcelain() == "" }, 5*time.Second) {
		t.Fatalf("not staged: %q", r.porcelain())
	}
	if !r.exists("main.go") {
		t.Error("main.go deleted")
	}
}
