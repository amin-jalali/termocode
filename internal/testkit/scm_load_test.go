package testkit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// holdFirstGitStatus puts a git wrapper first on PATH. The first
// `git status` run through it (the app's first background refresh) waits
// until the returned release func is called; every other git call runs at
// once. Call it after the test repo is built.
func holdFirstGitStatus(t testing.TB) (release func()) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	dir := looseTempDir(t)
	gate := filepath.Join(dir, "gate")
	if err := os.WriteFile(gate, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = status ] && mkdir '" + filepath.Join(dir, "first") + "' 2>/dev/null; then\n" +
		"  while [ -e '" + gate + "' ]; do sleep 0.02; done\n" +
		"fi\n" +
		"exec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	release = func() { _ = os.Remove(gate) }
	t.Cleanup(release)
	return release
}

// Spec: Given the first background git refresh has not finished yet, when
// the user opens Source Control and at once stages a file with the panel
// keys, commits it from the message box and pushes, then each of these
// actions runs: the file is committed and origin has the new commit. While
// git loads, the panel does not claim the folder is not a repository.
func TestSCMActionsWorkBeforeFirstRefresh(t *testing.T) {
	var bare string
	var release func()
	d, r := scmStart(t, func(r *repo) {
		bare = r.addOrigin()
		r.write("main.go", "package main\n// edit\n")
		release = holdFirstGitStatus(t)
	})
	defer release()
	d.Palette("git-focus")
	if strings.Contains(text(d), "Not a git repository") {
		t.Errorf("while git loads the panel says it is not a repo:%s", dump(d))
	}
	d.Key("down", "s")
	if got := r.porcelain(); got != "M  main.go" {
		t.Errorf("down, s before the first refresh: git status %q, want %q", got, "M  main.go")
	}
	d.Key("c")
	d.Type("feat: early")
	d.Key("enter")
	if got := r.git("log", "-1", "--format=%s"); got != "feat: early" {
		t.Errorf("commit before the first refresh: HEAD is %q:%s", got, dump(d))
	}
	d.Palette("git-push")
	if !until(d, func() bool { return strings.Contains(text(d), "Pushed") }) {
		t.Errorf("push before the first refresh did not run:%s", dump(d))
	}
	if got, want := r.git("--git-dir", bare, "rev-parse", "main"), r.git("rev-parse", "HEAD"); got != want {
		t.Errorf("origin main = %s, want our HEAD %s", got, want)
	}
}
