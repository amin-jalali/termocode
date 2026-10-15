package testkit

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// bigHistory adds n linear commits on top of HEAD with one git
// fast-import run (seconds faster than n commits), then resets the work
// tree to the new HEAD.
func bigHistory(r *repo, n int) {
	r.t.Helper()
	parent := r.git("rev-parse", "HEAD")
	start := r.clock.Unix()
	var b strings.Builder
	for i := 1; i <= n; i++ {
		msg := fmt.Sprintf("change number %d\n", i)
		body := fmt.Sprintf("line %d\n", i)
		ts := start + int64(i)*60
		fmt.Fprintf(&b, "commit refs/heads/main\nmark :%d\n", i)
		fmt.Fprintf(&b, "author Test <test@example.com> %d +0000\ncommitter Test <test@example.com> %d +0000\n", ts, ts)
		fmt.Fprintf(&b, "data %d\n%s", len(msg), msg)
		if i == 1 {
			fmt.Fprintf(&b, "from %s\n", parent)
		} else {
			fmt.Fprintf(&b, "from :%d\n", i-1)
		}
		fmt.Fprintf(&b, "M 644 inline big%d.txt\ndata %d\n%s\n", i%20, len(body), body)
	}
	cmd := exec.Command("git", "fast-import", "--quiet", "--force")
	cmd.Dir = r.dir
	cmd.Stdin = strings.NewReader(b.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		r.t.Fatalf("fast-import: %v\n%s", err, out)
	}
	r.git("reset", "-q", "--hard")
}

// Perf guard (scm-graph design §4.6, P1): with a 2 000-commit history
// the GRAPH loads its first page (header "200+"), and "Load older
// commits" adds the next page quickly. Budget 100 ms a page; the guard
// is 1 s so -race and busy CI boxes do not flake, but a page that walks
// the whole history or blocks the UI loop fails.
func TestSCMGraphBigHistoryFast(t *testing.T) {
	d, _ := scmDriver(t, func(r *repo) { bigHistory(r, 2000) })
	if !until(d, func() bool { return strings.Contains(sidebarRow(d, "GRAPH"), "200+") }) {
		t.Fatalf("GRAPH header does not say 200+:%s", dump(d))
	}
	scmClickText(d, "change num") // the newest commit: the cursor (opens the view)
	d.Key("esc")
	// G puts the cursor (and the view) on the last row: "Show 15 more"
	// until all 200 are shown, then "Load older commits".
	for i := 0; i < 20; i++ {
		d.Key("G")
		if strings.Contains(text(d), "Load older commits") {
			break
		}
		d.Key("enter")
	}
	if !strings.Contains(text(d), "Load older commits") || strings.Contains(text(d), "change number 1800") {
		t.Fatalf("no Load older commits row after showing 200:%s", dump(d))
	}
	t0 := time.Now()
	d.Key("enter")
	if !until(d, func() bool { return strings.Contains(text(d), "change number 1800") }) {
		t.Fatalf("Load older did not add a page:%s", dump(d))
	}
	if took := time.Since(t0); took > time.Second {
		t.Errorf("Load older took %v (budget 100 ms, guard 1 s)", took)
	}
}

// sidebarRow is the sidebar row that holds s ("" when none).
func sidebarRow(d *Driver, s string) string {
	for _, l := range sidebar(d) {
		if strings.Contains(l, s) {
			return l
		}
	}
	return ""
}
