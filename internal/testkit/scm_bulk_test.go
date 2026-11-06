package testkit

import (
	"strings"
	"testing"
	"time"
)

// Source Control: whole-tree actions from the ⋯ menu (GITX-7, GITX-20).

// Spec: Given a changed file and a new file, when the user picks Discard
// All Changes… from the ⋯ menu, then a Cancel-first card asks; Esc and
// Enter keep the change; Discard Changes takes it back, keeps the new
// file and leaves a safety copy in the stash list.
func TestSCMDiscardAllFromMenu(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n// edit\n")
		r.write("new.txt", "new\n")
	})
	d.WaitForText("main.go", 3*time.Second)
	openMoreMenu(d)
	clickScreen(d, "Discard All Changes…")
	if !strings.Contains(text(d), "Discard all changes?") {
		t.Fatalf("no card:%s", dump(d))
	}
	d.Key("esc")
	openMoreMenu(d)
	clickScreen(d, "Discard All Changes…")
	d.Key("enter") // Cancel is the ready button
	if r.read("main.go") != "package main\n// edit\n" {
		t.Fatal("Esc / Enter discarded the change")
	}
	openMoreMenu(d)
	clickScreen(d, "Discard All Changes…")
	d.Key("right") // Discard Changes
	d.Key("enter")
	until(d, func() bool { return r.read("main.go") == scmFiles["main.go"] })
	if got := r.read("main.go"); got != scmFiles["main.go"] || !r.exists("new.txt") {
		t.Errorf("main.go = %q, new.txt kept = %v", got, r.exists("new.txt"))
	}
	if got := r.git("stash", "list"); !strings.Contains(got, "safety copy") {
		t.Errorf("no safety stash: %q", got)
	}
}

// Spec: Given a staged file and an origin, when the user types a message
// and picks Commit and Push from the ⋯ menu, then origin has the commit.
func TestSCMCommitAndPushFromMenu(t *testing.T) {
	var bare string
	d, r := scmDriver(t, func(r *repo) {
		bare = r.addOrigin()
		r.write("main.go", "package main\n// pushed\n")
		r.git("add", "main.go")
	})
	d.WaitForText("main.go", 3*time.Second)
	typeCommitMessage(d, "feat: pushed")
	d.Key("esc")
	openMoreMenu(d)
	clickScreen(d, "Commit and Push")
	if !d.WaitFor(func() bool {
		out, _ := r.try("--git-dir="+bare, "log", "-1", "--format=%s", "main")
		return strings.TrimSpace(out) == "feat: pushed"
	}, 5*time.Second) {
		t.Fatalf("origin did not get the commit:%s", dump(d))
	}
}
