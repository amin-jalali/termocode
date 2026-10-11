package testkit

import (
	"strings"
	"testing"
)

// Source Control: git failures show a toast that says why.

// Spec: Given origin moved on and we have our own commit, when the user
// pushes, then git refuses (non-fast-forward), an error toast says why (not
// just git's "To <url>" first line), and origin is unchanged.
func TestSCMPushRejectedShowsError(t *testing.T) {
	var bare string
	d, r := scmDriver(t, func(r *repo) {
		bare = r.addOrigin()
		r.write("local.txt", "l\n")
		r.commit("feat: local")
	})
	o := r.otherClone(bare)
	o.write("remote.txt", "r\n")
	o.commit("feat: remote")
	o.git("push", "-q")
	remoteHead := o.git("rev-parse", "HEAD")

	d.Palette("git-push")
	if !until(d, func() bool { return strings.Contains(text(d), "Push failed") }) {
		t.Errorf("no Push failed toast:%s", dump(d))
	}
	if !strings.Contains(text(d), "rejected") {
		t.Errorf("toast does not say the push was rejected:%s", dump(d))
	}
	if got := r.git("--git-dir", bare, "rev-parse", "main"); got != remoteHead {
		t.Errorf("origin main moved to %s", got)
	}
}
