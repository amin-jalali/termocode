package testkit

import (
	"strings"
	"testing"
)

// Source Control: merge conflicts.

// conflictRepo leaves a merge of "theirs" into main stopped on a conflict
// in main.go, with merged.txt (no conflict) staged by the merge.
func conflictRepo(r *repo) {
	r.git("checkout", "-q", "-b", "theirs")
	r.write("main.go", "package main\n\nfunc main() { println(\"theirs\") }\n")
	r.write("merged.txt", "clean\n")
	r.commit("feat: theirs")
	r.git("checkout", "-q", "main")
	r.write("main.go", "package main\n\nfunc main() { println(\"ours\") }\n")
	r.commit("feat: ours")
	if _, err := r.try("merge", "--no-edit", "theirs"); err == nil {
		r.t.Fatal("setup: merge did not conflict")
	}
}

// Spec: Given a merge stopped on a conflict, when Source Control shows,
// then a CONFLICTS section (first, count 1) lists the conflicted file, which
// is not also listed under STAGED or CHANGES, and the cleanly merged file is
// under STAGED; when the user resolves and adds the file in a terminal and
// git state refreshes, then CONFLICTS goes away and the file is staged.
func TestSCMConflictsSection(t *testing.T) {
	d, r := scmDriver(t, conflictRepo)
	if got := r.porcelain(); got != "UU main.go|A  merged.txt" {
		t.Fatalf("setup: git status %q", got)
	}
	if !until(d, func() bool { return section(d, "CONFLICTS") != nil }) {
		t.Fatalf("no CONFLICTS section:%s", dump(d))
	}
	if c, s := mustRow(d, "CONFLICTS"), mustRow(d, "STAGED"); c > s {
		t.Errorf("CONFLICTS is not the first section")
	}
	if y := mustRow(d, "CONFLICTS"); !strings.HasSuffix(strings.TrimSpace(sidebar(d)[y]), "1") {
		t.Errorf("CONFLICTS count: %q", sidebar(d)[y])
	}
	if !inSection(d, "CONFLICTS", "main.go") {
		t.Errorf("main.go not under CONFLICTS:%s", dump(d))
	}
	if inSection(d, "STAGED", "main.go") || inSection(d, "CHANGES", "main.go") {
		t.Errorf("conflicted main.go also listed as staged/changed:%s", dump(d))
	}
	if !inSection(d, "STAGED", "merged.txt") {
		t.Errorf("cleanly merged file not staged:%s", dump(d))
	}

	r.write("main.go", "package main\n\nfunc main() { println(\"both\") }\n")
	r.git("add", "main.go")
	d.Palette("git-refresh")
	until(d, func() bool { return section(d, "CONFLICTS") == nil })
	if section(d, "CONFLICTS") != nil {
		t.Errorf("CONFLICTS still shown after resolving:%s", dump(d))
	}
	if !inSection(d, "STAGED", "main.go") {
		t.Errorf("resolved main.go not staged:%s", dump(d))
	}
	// Committing finishes the merge: a commit with two parents.
	typeCommitMessage(d, "Merge branch 'theirs'")
	d.Key("enter")
	until(d, func() bool { return r.porcelain() == "" })
	if got := r.git("rev-list", "--parents", "-n1", "HEAD"); len(strings.Fields(got)) != 3 {
		t.Errorf("merge commit has parents %q, want 2", got)
	}
}
