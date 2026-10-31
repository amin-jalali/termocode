//go:build unix

package mcp

import (
	"os"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// userShared is the fake's entry with "share": "user".
func userShared(e *managerEnv, share string) map[string]any {
	entry := mcptest.Entry(map[string]string{"FAKE_LOG": e.log})
	if share != "" {
		entry["share"] = share
	}
	return entry
}

// Spec (MCPX-29, RT-16): by default each project (other roots) runs its
// own process; "share": "user" makes one process serve every project.
func TestManagerShareUserServesEveryProject(t *testing.T) {
	for _, c := range []struct {
		share  string
		starts int
	}{{"", 2}, {ShareUser, 1}} {
		e := newManagerEnv(t)
		e.write(t, e.user, map[string]any{"fake": userShared(e, c.share)})
		e2 := newManagerEnv(t) // another project folder
		e2.log = e.log
		e2.write(t, e2.user, map[string]any{"fake": userShared(e2, c.share)})
		share := shareTestOptions(t)
		a := sharedManager(t, e, share, Expander{Env: os.LookupEnv})
		b := sharedManager(t, e2, share, Expander{Env: os.LookupEnv})
		callEcho(t, a, "x")
		callEcho(t, b, "y")
		if n := countLines(e.log, "initialize"); n != c.starts {
			t.Fatalf("share %q: %d starts, want %d", c.share, n, c.starts)
		}
		a.Close()
		b.Close()
	}
}
