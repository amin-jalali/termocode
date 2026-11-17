//go:build unix

package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// git.branch_create (the phone's "+ New branch from main"): a bad name is
// refused by the args rules, a name taken is refused, a new one waits
// for the laptop's card and then the laptop is on it, the changed files
// with it.
func TestGitBranchCreateWaitsForCard(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	run("branch", "feat/web")
	webTestWrite(t, dir, "a.txt", "changed\n")
	webTestGitLoad(&m)
	c := webTestSub(t, &m)
	for _, bad := range []string{`""`, `"-x"`, `"a b"`, `"a..b"`, `"x.lock"`} {
		if _, err := webproto.DecodeArgs(webproto.OpGitBranchCreate, []byte(`{"name":`+bad+`}`)); err == nil {
			t.Errorf("name %s decoded", bad)
		}
	}
	var br webproto.GitBranches
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitBranches, `{}`), &br)
	for _, taken := range []string{"main", "feat/web"} {
		if f := webTestCallBg(t, &m, c, webproto.OpGitBranchCreate, `{"name":"`+taken+`"}`); webTestErr(f) != "conflict" {
			t.Errorf("%s again: %v", taken, f)
		}
	}
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitBranchCreate, `{"name":"feature/checkout"}`), &ref)
	if v := m.confirm.View(); !strings.Contains(v, "Make the branch feature/checkout from main and switch") || !strings.Contains(v, "1 changed file go with you") {
		t.Errorf("card:\n%s", v)
	}
	if got := strings.TrimSpace(run("rev-parse", "--abbrev-ref", "HEAD")); got != "main" {
		t.Fatalf("on %q before the laptop's yes", got)
	}
	webTestLaptopConfirmRemote(t, &m)
	if got := strings.TrimSpace(run("rev-parse", "--abbrev-ref", "HEAD")); got != "feature/checkout" {
		t.Fatalf("on %q after the laptop's yes", got)
	}
	if st := run("status", "--short"); !strings.Contains(st, "a.txt") {
		t.Errorf("the change did not go along: %q", st)
	}
}
