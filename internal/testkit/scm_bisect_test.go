package testkit

import (
	"strings"
	"testing"
)

// Source Control: bisect (GITX-19).

// Spec: Bisect… marks HEAD bad and asks for a good commit; then a picker
// shows the commit under test with Good, Bad, Skip, Run a Task and End
// Bisect; End Bisect goes back to the branch.
func TestSCMBisectStartAndEnd(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		for _, s := range []string{"two", "three", "four", "five"} {
			r.write(s+".txt", s+"\n")
			r.commit("add " + s)
		}
	})
	d.Palette("git-bisect")
	if !until(d, func() bool { return strings.Contains(text(d), "still good") }) {
		t.Fatalf("no good-commit picker:%s", dump(d))
	}
	d.Type("init")
	d.Key("enter")
	if !until(d, func() bool { return strings.Contains(text(d), "Good: this commit works") }) {
		t.Fatalf("no step picker:%s", dump(d))
	}
	if r.git("rev-parse", "--abbrev-ref", "HEAD") != "HEAD" {
		t.Error("git did not check out a commit to test")
	}
	d.Type("End Bisect")
	d.Key("enter")
	if !until(d, func() bool { return r.git("rev-parse", "--abbrev-ref", "HEAD") == "main" }) {
		t.Errorf("not back on main:%s", dump(d))
	}
}
