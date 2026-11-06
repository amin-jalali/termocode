package testkit

import (
	"testing"
	"time"
)

// Source Control: merge view tools (GITX-1 I10, GITX-34). Real nvim and
// git; skipped with -short.

// Spec: Given a merge view on a file with two conflicts, when the user
// presses T, then both take theirs in one step and u brings both back;
// when the user presses O and saves, then the file has ours everywhere.
func TestSCMMergeViewTakeAll(t *testing.T) {
	d, r := mergeDriver(t, 150, 42, twoConflictRepo)
	clickConflict(d)
	waitMerge(d, "Conflict 1 of 2")
	d.Key("T")
	waitMerge(d, "No conflicts left")
	d.Key("u")
	waitMerge(d, "Conflict 1 of 2")
	d.Key("O")
	waitMerge(d, "No conflicts left")
	d.Key("ctrl+s")
	want := "package main\n\nfunc a() { println(\"a-ours\") }\n\n// 1\n// 2\n// 3\n// 4\n// 5\n// 6\n\nfunc b() { println(\"b-ours\") }\n"
	if !d.WaitFor(func() bool { return r.read("main.go") == want }, 5*time.Second) {
		t.Fatalf("main.go = %q, want %q", r.read("main.go"), want)
	}
}

// Spec: Given a conflict where both sides changed the line differently,
// when the user presses s, then it says there is no simple conflict and
// the conflict stays.
func TestSCMMergeViewResolveSimpleKeepsReal(t *testing.T) {
	d, _ := mergeDriver(t, 150, 42, conflictRepo)
	clickConflict(d)
	waitMerge(d, "Conflict 1 of 1")
	d.Key("s")
	waitMerge(d, "No simple conflicts")
	waitMerge(d, "Conflict 1 of 1")
}
