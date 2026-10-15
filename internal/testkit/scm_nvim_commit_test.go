package testkit

import (
	"strings"
	"testing"
)

// Spec: Given a commit in the GRAPH, when the user clicks it and then Raw
// on the commit card, then the card closes and the editor shows that
// commit's full diff (git show): its hash, message and the changed
// file's +lines.
func TestSCMNvimClickCommitOpensDiff(t *testing.T) {
	d, r := scmNvimDriver(t, func(r *repo) {
		r.write("parser.go", "package main\n\nfunc parse() {}\n")
		r.commit("feat: add parser")
	})
	hash := r.git("rev-parse", "HEAD")
	scmClickText(d, "add parser")
	if !waitText(d, " Raw ") {
		t.Fatalf("no commit card:%s", dump(d))
	}
	clickScreen(d, " Raw ")
	if !waitText(d, "commit "+hash) {
		t.Fatalf("commit diff not shown:%s", dump(d))
	}
	d.Move(120, 40) // off the graph: the hover card covers the editor
	for _, want := range []string{"feat: add parser", "+++ b/parser.go", "+func parse() {}", hash[:7] + " · commit"} {
		if !strings.Contains(text(d), want) {
			t.Errorf("commit view lacks %q:%s", want, dump(d))
		}
	}
}
