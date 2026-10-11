package testkit

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/clock"
)

// Source Control behaviour that needs the embedded nvim: the editor's diff
// views, gutter signs, blame and conflict resolution. Skipped with -short.

// scmNvimDriver is scmDriver with a real nvim.
func scmNvimDriver(t *testing.T, setup func(r *repo)) (*Driver, *repo) {
	t.Helper()
	if testing.Short() {
		t.Skip("starts nvim")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	old := clock.Now
	clock.Now = func() time.Time { return scmNow }
	t.Cleanup(func() { clock.Now = old })
	t.Setenv("TZ", "UTC")
	var r *repo
	d := New(t, Options{W: 140, H: 44, Nvim: true, Files: scmFiles, Setup: func(tb testing.TB, dir string) {
		r = newRepo(tb, dir)
		r.commit("init")
		if setup != nil {
			setup(r)
		}
	}})
	d.Action("view:git")
	if !d.WaitFor(func() bool { d.render(); return section(d, "GRAPH") != nil && !inSection(d, "GRAPH", "No commits") }, 5*time.Second) {
		t.Fatalf("Source Control did not load:%s", dump(d))
	}
	return d, r
}

// waitText waits up to 5s for s anywhere on screen.
func waitText(d *Driver, s string) bool {
	d.T.Helper()
	return d.WaitForText(s, 5*time.Second)
}

// editorRow returns the frame row (ANSI kept) that shows s right of the
// sidebar, and its index, or ("", -1).
func editorRow(d *Driver, s string) (string, int) {
	for y, l := range strings.Split(d.Frame(), "\n") {
		plain := ansi.Strip(l)
		if i := strings.Index(plain, "▕"); i >= 0 {
			plain = plain[i:]
		}
		if strings.Contains(plain, s) {
			return l, y
		}
	}
	return "", -1
}

// signBefore returns the plain text left of s on its editor row (the
// sign column and line number), after the sidebar edge.
func signBefore(d *Driver, s string) string {
	row, y := editorRow(d, s)
	if y < 0 {
		return "<missing>"
	}
	plain := ansi.Strip(row)
	if i := strings.Index(plain, "▕"); i >= 0 {
		plain = plain[i+len("▕"):]
	}
	return plain[:strings.Index(plain, s)]
}

// Spec: Given a modified file, when the user clicks it in CHANGES, then the
// editor shows a side-by-side diff: the HEAD text in a left pane labelled
// HEAD and the working text in a right pane labelled working.
func TestSCMNvimClickFileOpensSideBySideDiff(t *testing.T) {
	d, _ := scmNvimDriver(t, func(r *repo) {
		r.write("main.go", "package main\n\nfunc main() { println(\"new\") }\n")
	})
	scmClickText(d, "main.go")
	if !waitText(d, "working") {
		t.Fatalf("no diff view:%s", dump(d))
	}
	for _, want := range []string{"main.go  ·  HEAD", "main.go  ·  working", "func main() {}", `println("new")`} {
		if !strings.Contains(text(d), want) {
			t.Errorf("diff view lacks %q:%s", want, dump(d))
		}
	}
	// HEAD (old) is left of the working (new) text.
	_, yOld := editorRow(d, "func main() {}")
	rowOld := ansi.Strip(strings.Split(d.Frame(), "\n")[yOld])
	if strings.Index(rowOld, "func main() {}") > strings.Index(rowOld, `println("new")`) && strings.Contains(rowOld, `println("new")`) {
		t.Errorf("HEAD pane is not on the left: %q", rowOld)
	}
}

// Spec: Given a merge conflict in main.go, when the user clicks it under
// CONFLICTS, then it opens in the editor; when the user goes to the
// conflict and picks "Accept Incoming Change" and saves, then the file has
// only the incoming text and no conflict markers.
func TestSCMNvimResolveConflictAcceptIncoming(t *testing.T) {
	d, r := scmNvimDriver(t, conflictRepo)
	until(d, func() bool { return inSection(d, "CONFLICTS", "main.go") })
	y := -1
	for i, l := range sidebar(d) {
		if strings.Contains(l, "main.go") {
			y = i
			break
		}
	}
	d.Click(colOf(d, y, "main.go"), y)
	if !waitText(d, "<<<<<<<") {
		t.Fatalf("conflict file not opened:%s", dump(d))
	}
	d.Palette("git-conflict-next")
	d.Palette("git-conflict-theirs")
	d.Key("esc")
	d.Type(":w")
	d.Key("enter")
	if !d.WaitFor(func() bool { return !strings.Contains(r.read("main.go"), "<<<<<<<") }, 5*time.Second) {
		t.Fatalf("file still has markers: %q", r.read("main.go"))
	}
	if got := r.read("main.go"); got != "package main\n\nfunc main() { println(\"theirs\") }\n" {
		t.Errorf("main.go = %q, want the incoming side", got)
	}
}

// Spec: Given main.go open with unsaved edits in the editor and a different
// change on disk, when the user stashes, then the disk change is stashed,
// the app does not hang on an nvim prompt, and the unsaved edits are still
// in the buffer (not silently lost).
func TestSCMNvimStashWithUnsavedBuffer(t *testing.T) {
	d, r := scmNvimDriver(t, func(r *repo) {
		r.write("README.md", "# Demo\nchanged on disk\n")
	})
	d.Action("open:main.go")
	waitText(d, "func main()")
	d.Key("G", "o")
	d.Type("// unsaved edit")
	d.Key("esc")
	if !waitText(d, "// unsaved edit") {
		t.Fatalf("edit not typed:%s", dump(d))
	}
	d.Palette("git-stash")
	until(d, func() bool { return strings.Contains(text(d), "Stashed") })
	if got := r.read("README.md"); got != scmFiles["README.md"] {
		t.Errorf("README.md = %q after stash", got)
	}
	if !strings.Contains(r.git("stash", "list"), "stash@{0}") {
		t.Error("nothing stashed")
	}
	if !strings.Contains(text(d), "// unsaved edit") {
		t.Errorf("unsaved buffer edit lost:%s", dump(d))
	}
	if got := r.read("main.go"); strings.Contains(got, "unsaved edit") {
		t.Errorf("stash wrote the unsaved edit to disk: %q", got)
	}
}

// Spec: Given main.go open in the editor and changed on disk (vs HEAD),
// when the user stashes, then the open buffer reloads to the committed
// text; when the user pops, then it shows the change again.
func TestSCMNvimStashReloadsOpenBuffer(t *testing.T) {
	d, r := scmNvimDriver(t, func(r *repo) {
		r.write("main.go", "package main\n\nfunc main() { println(\"wip\") }\n")
	})
	d.Action("open:main.go")
	waitText(d, `println("wip")`)
	d.Palette("git-stash")
	if !waitText(d, "func main() {}") {
		t.Errorf("buffer not reloaded after stash:%s", dump(d))
	}
	d.Palette("git-stash-pop")
	if !waitText(d, `println("wip")`) {
		t.Errorf("buffer not reloaded after pop:%s", dump(d))
	}
	if got := r.read("main.go"); !strings.Contains(got, "wip") {
		t.Errorf("pop did not restore main.go: %q", got)
	}
}
