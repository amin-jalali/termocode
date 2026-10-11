package testkit

import (
	"strings"
	"testing"
)

// Source Control state shown outside the panel: explorer badges and the
// status-bar branch.

// explorerBadge returns the last non-blank cell of the Files explorer row
// that shows name (the git badge column), or "" when the row has none.
func explorerBadge(d *Driver, name string) string {
	d.T.Helper()
	for _, l := range sidebar(d) {
		if !strings.Contains(l, " "+name) {
			continue
		}
		rest := strings.TrimSpace(l[strings.Index(l, " "+name)+len(name)+1:])
		if rest == "" {
			return ""
		}
		f := strings.Fields(rest)
		return f[len(f)-1]
	}
	d.T.Fatalf("%q not in the explorer:%s", name, dump(d))
	return ""
}

// Spec: Given a modified, a staged-new, an untracked and an unchanged file,
// when the Files explorer shows, then each changed file has its git letter
// (M, A, U) at the right and the unchanged file has none; when the changes
// are committed outside the app and git state refreshes, the letters go.
func TestSCMExplorerBadges(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n// m\n")
		r.write("added.go", "package main\n")
		r.git("add", "added.go")
		r.write("notes.txt", "n\n")
		r.write("my notes.md", "n\n")
	})
	d.Action("view:files")
	until(d, func() bool { return strings.Contains(text(d), "notes.txt") })
	for name, want := range map[string]string{
		"main.go": "M", "added.go": "A", "notes.txt": "U", "my notes.md": "U", "README.md": "",
	} {
		if got := explorerBadge(d, name); got != want {
			t.Errorf("%s: badge %q, want %q", name, got, want)
		}
	}
	r.commit("chore: everything")
	d.Palette("git-refresh")
	until(d, func() bool { return explorerBadge(d, "main.go") == "" })
	for _, name := range []string{"main.go", "added.go", "notes.txt"} {
		if got := explorerBadge(d, name); got != "" {
			t.Errorf("%s: badge %q after commit, want none", name, got)
		}
	}
}

// Spec: Given a detached HEAD (a tag checked out), when the app shows, then
// the status bar says "detached" with the short hash instead of a branch.
func TestSCMStatusBarDetachedHead(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("a.txt", "a\n")
		r.commit("feat: a")
		r.git("tag", "v1")
		r.git("checkout", "-q", "v1~1")
	})
	short := r.git("rev-parse", "--short", "HEAD")
	until(d, func() bool { return strings.Contains(statusBar(d), short) })
	if !strings.Contains(statusBar(d), "detached "+short) {
		t.Errorf("status bar = %q, want detached %s", statusBar(d), short)
	}
}

// Spec: Given the user switches branch in a terminal (outside the app),
// when the git state is refreshed (the 2-second poll or Refresh), then the
// status bar and the panel show the new branch.
func TestSCMStatusBarFollowsOutsideCheckout(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) { r.git("branch", "dev") })
	if !strings.Contains(statusBar(d), "main") {
		t.Fatalf("status bar = %q", statusBar(d))
	}
	r.git("checkout", "-q", "dev")
	d.Palette("git-refresh")
	if !until(d, func() bool { return strings.Contains(statusBar(d), "dev") }) {
		t.Errorf("status bar = %q after checkout dev", statusBar(d))
	}
	if !strings.Contains(sidebar(d)[2], "dev") {
		t.Errorf("panel branch row = %q", sidebar(d)[2])
	}
}
