package testkit

import (
	"strings"
	"testing"
)

// Source Control: log search and filters (GITX-9).

// Spec: Search Log… takes one line of text and filters; the list shows
// only the matching commits, and Enter opens the picked one.
func TestSCMSearchLog(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("b.txt", "b\n")
		r.commit("docs: explain b")
		r.write("c.txt", "c\n")
		r.commit("fix: crash in c")
	})
	fix := r.git("rev-parse", "--short", "HEAD")
	d.Palette("git-log-search")
	d.Type("crash")
	d.Key("enter")
	if !until(d, func() bool { return strings.Contains(text(d), fix+"  fix: crash in c") }) {
		t.Fatalf("no result:%s", dump(d))
	}
	if strings.Contains(text(d), "docs: explain b") {
		t.Errorf("a commit that does not match is listed:%s", dump(d))
	}
	d.Key("enter")
	if !until(d, func() bool { return strings.Contains(text(d), "c.txt") }) {
		t.Errorf("Enter did not open the commit:%s", dump(d))
	}
}
