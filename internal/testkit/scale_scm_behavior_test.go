package testkit

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/activity"
)

// fiftyKDeleted commits n files with git plumbing (one shared blob, one
// update-index run) without writing them to the work tree, so git status
// lists n deleted files. Seconds faster than writing and adding them.
func fiftyKDeleted(r *repo, n int) {
	r.t.Helper()
	blob := strings.TrimSpace(r.gitIn("x\n", "hash-object", "-w", "--stdin"))
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "100644 %s\tgen/d%03d/f%05d.txt\n", blob, i/500, i)
	}
	r.gitIn(b.String(), "update-index", "--add", "--index-info")
	tree := r.git("write-tree")
	head := r.git("rev-parse", "HEAD")
	c := r.gitIn("50k files\n", "commit-tree", tree, "-p", head)
	r.git("update-ref", "HEAD", strings.TrimSpace(c))
}

// gitIn runs git with stdin and returns its output.
func (r *repo) gitIn(stdin string, args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// Spec (SCN-12): Given a repo where git status lists 50 000 changed
// files,
// when the user opens Source Control and moves through the list,
// then the view shows the changes within its bound, keys and the wheel
// do not block the UI loop and the Go heap stays bounded.
//
// Local (race off): repo made 0.15 s, changes shown 0.4 s, worst key or
// wheel 3 ms (it was 170 ms before the row list was kept), heap +18 MB.
// Quick: it also runs with -short.
func TestScaleSCM50kChanges(t *testing.T) {
	base := heapMB()
	t0 := time.Now() // start-up and the first git status
	var made time.Time
	d, _ := scmStart(t, func(r *repo) { fiftyKDeleted(r, 50_000); made = time.Now() })
	t.Logf("repo made in %v, app up %v later", made.Sub(t0), time.Since(made))
	t0 = made
	if !d.WaitFor(func() bool { d.render(); return strings.Contains(text(d), "f00000.txt") }, 20*time.Second*raceFactor()) {
		t.Fatalf("the changes did not show:%s", dump(d))
	}
	within(t, "start and show 50k changes", time.Since(t0), 2*time.Second)
	scmClickText(d, "f00000.txt")
	d.Key("esc")
	key := worstKey(d, "down", "down", "pgdown", "pgdown", "end", "home", "up")
	for i := 0; i < 10; i++ {
		key = max(key, d.Timed(wheel(activity.Width+5, d.H/2, false)))
	}
	d.Settle(200 * time.Millisecond)
	within(t, "worst key or wheel", key, 100*time.Millisecond)
	heapWithin(t, "50k changes", base, 60)
}
