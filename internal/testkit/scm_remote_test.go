package testkit

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Source Control against a remote: ahead/behind, push, pull, fetch. The
// remote is a local bare repo — no network.

// statusBar is the last row of the frame (ANSI stripped).
func statusBar(d *Driver) string {
	rows := strings.Split(text(d), "\n")
	return rows[len(rows)-1]
}

// Spec: Given main tracks origin/main and has one local commit, when Source
// Control shows, then the status bar and the panel say "↑1"; given origin
// has a commit we do not have, when the user fetches, then they say "↓1"
// and the working tree is untouched.
func TestSCMAheadBehindAndFetch(t *testing.T) {
	var bare string
	d, r := scmDriver(t, func(r *repo) {
		bare = r.addOrigin()
		r.write("local.txt", "l\n")
		r.commit("feat: local")
	})
	if !until(d, func() bool { return strings.Contains(statusBar(d), "main ↑1") }) {
		t.Fatalf("status bar does not show ↑1: %q", statusBar(d))
	}
	if !strings.Contains(sidebar(d)[2], "main ↑1") {
		t.Errorf("panel branch row does not show ↑1: %q", sidebar(d)[2])
	}

	o := r.otherClone(bare)
	o.write("remote.txt", "r\n")
	o.commit("feat: remote")
	o.git("push", "-q")

	d.Palette("git-fetch")
	if !until(d, func() bool { return strings.Contains(statusBar(d), "main ↑1 ↓1") }) {
		t.Errorf("after fetch, status bar = %q, want ↑1 ↓1", statusBar(d))
	}
	if !strings.Contains(text(d), "Fetched") {
		t.Errorf("no Fetched toast:%s", dump(d))
	}
	if r.exists("remote.txt") {
		t.Error("fetch changed the working tree")
	}
}

// Spec: Given main is one commit ahead of origin, when the user pushes,
// then origin/main (in the bare repo) is our HEAD and "↑1" goes away.
func TestSCMPush(t *testing.T) {
	var bare string
	d, r := scmDriver(t, func(r *repo) {
		bare = r.addOrigin()
		r.write("local.txt", "l\n")
		r.commit("feat: local")
	})
	until(d, func() bool { return strings.Contains(statusBar(d), "↑1") })
	d.Palette("git-push")
	if !until(d, func() bool { return !strings.Contains(statusBar(d), "↑") && strings.Contains(text(d), "Pushed") }) {
		t.Errorf("after push: status bar %q:%s", statusBar(d), dump(d))
	}
	if got, want := r.git("--git-dir", bare, "rev-parse", "main"), r.git("rev-parse", "HEAD"); got != want {
		t.Errorf("origin main = %s, want our HEAD %s", got, want)
	}
}

// Spec: Given a new branch with no upstream, when the user pushes, then the
// branch is created on origin and set as the upstream.
func TestSCMPushNewBranchSetsUpstream(t *testing.T) {
	var bare string
	d, r := scmDriver(t, func(r *repo) {
		bare = r.addOrigin()
		r.git("checkout", "-q", "-b", "topic")
		r.write("t.txt", "t\n")
		r.commit("feat: topic")
	})
	d.Palette("git-push")
	until(d, func() bool { return strings.Contains(text(d), "Pushed") })
	if got, want := r.git("--git-dir", bare, "rev-parse", "topic"), r.git("rev-parse", "HEAD"); got != want {
		t.Errorf("origin topic = %s, want %s", got, want)
	}
	if got := r.git("rev-parse", "--abbrev-ref", "@{upstream}"); got != "origin/topic" {
		t.Errorf("upstream = %q, want origin/topic", got)
	}
}

// Spec: Given origin has a commit we do not have (and we have no local
// commits), when the user pulls, then the commit is in our history, its
// file is on disk and "↓" is gone.
func TestSCMPull(t *testing.T) {
	var bare string
	d, r := scmDriver(t, func(r *repo) {
		bare = r.addOrigin()
	})
	o := r.otherClone(bare)
	o.write("remote.txt", "from origin\n")
	o.commit("feat: remote")
	o.git("push", "-q")

	d.Palette("git-pull")
	if !until(d, func() bool { return inSection(d, "GRAPH", "remote") }) {
		t.Errorf("pulled commit not in the graph:%s", dump(d))
	}
	if got := r.read("remote.txt"); got != "from origin\n" {
		t.Errorf("remote.txt = %q after pull", got)
	}
	if got, want := r.git("rev-parse", "HEAD"), o.git("rev-parse", "HEAD"); got != want {
		t.Errorf("HEAD %s, want origin's %s", got, want)
	}
	if strings.Contains(statusBar(d), "↓") {
		t.Errorf("status bar still behind: %q", statusBar(d))
	}
}

// Spec: Given main tracks origin and both sides have new commits on the
// same line, when the user runs Sync, then pull fails with a conflict
// message (no editor opens, nothing hangs) and the push does not run.
func TestSCMSyncStopsWhenPullFails(t *testing.T) {
	var bare string
	d, r := scmDriver(t, func(r *repo) {
		bare = r.addOrigin()
		r.git("config", "pull.rebase", "false")
		r.write("main.go", "package main\n// ours\n")
		r.commit("feat: ours")
	})
	o := r.otherClone(bare)
	o.write("main.go", "package main\n// theirs\n")
	o.commit("feat: theirs")
	o.git("push", "-q")
	remoteHead := o.git("rev-parse", "HEAD")

	d.Palette("git-sync")
	if !until(d, func() bool { return strings.Contains(text(d), "Sync failed") }) {
		t.Errorf("no Sync failed toast:%s", dump(d))
	}
	if got := r.git("--git-dir", bare, "rev-parse", "main"); got != remoteHead {
		t.Errorf("sync pushed although the pull failed")
	}
	// The failed merge leaves the conflict for the user to resolve.
	if !until(d, func() bool { return section(d, "CONFLICTS") != nil }) {
		t.Errorf("no CONFLICTS section after the failed pull:%s", dump(d))
	}
}

// Spec: Given a push that is still running (origin's pre-receive hook
// waits), when the user presses Push again and then Pull, then no second
// git process starts (the hook ran once) and a note says a git operation
// is already running; when the first push ends, "Pushed" shows and a new
// Push may run again.
func TestSCMSecondRemoteOpWhileOneRunsIsRefused(t *testing.T) {
	var bare string
	d, r := scmDriver(t, func(r *repo) {
		bare = r.addOrigin()
		r.write("local.txt", "l\n")
		r.commit("feat: local")
	})
	dir := looseTempDir(t)
	log, gate := dir+"/hook.log", dir+"/gate"
	if err := writeExec(bare+"/hooks/pre-receive", "#!/bin/sh\necho run >> '"+log+"'\n"+
		"while [ -e '"+gate+"' ]; do sleep 0.02; done\ncat >/dev/null\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeExec(gate, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeFile(gate) })

	d.Palette("git-push")
	if !until(d, func() bool { return readFile(log) == "run\n" }) {
		t.Fatalf("first push did not reach origin: hook log %q", readFile(log))
	}
	d.Palette("git-push")
	d.Palette("git-pull")
	if !strings.Contains(text(d), "already running") {
		t.Errorf("no 'already running' note for the second press:%s", dump(d))
	}
	d.Settle(300 * time.Millisecond)
	if got := readFile(log); got != "run\n" {
		t.Errorf("hook ran %d times, want 1 (a second git process started)", strings.Count(got, "run"))
	}
	removeFile(gate)
	if !until(d, func() bool { return strings.Contains(text(d), "Pushed") }) {
		t.Fatalf("first push did not finish:%s", dump(d))
	}
	r.write("more.txt", "m\n")
	r.commit("feat: more")
	d.Palette("git-push")
	if !until(d, func() bool { return strings.Count(readFile(log), "run") == 2 }) {
		t.Errorf("push after the first one ended did not run: hook log %q", readFile(log))
	}
}

func writeExec(path, body string) error { return os.WriteFile(path, []byte(body), 0o755) }

func readFile(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

func removeFile(path string) { _ = os.Remove(path) }
