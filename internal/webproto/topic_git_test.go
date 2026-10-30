package webproto

import (
	"strings"
	"testing"
)

// Spec (WEB-23): the git data is typed for the page: the git topic (its
// delta is the whole snapshot), the diff pages and the results of commit
// and of an op that waits for the laptop.
func TestGitDataGenerated(t *testing.T) {
	b, err := GenerateTS()
	if err != nil {
		t.Fatal(err)
	}
	ts := string(b)
	for _, want := range []string{
		`  "git": { snap: GitSnap; delta: GitSnap };`,
		`  "git.diff": GitDiff;`,
		`  "git.show": GitDiff;`,
		`  "git.commit": GitCommitted;`,
		`  "git.discard": AskRef;`,
		`  k: "ctx" | "add" | "del" | "hunk" | "file";`,
		`  op?: "rebase" | "merge";`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("proto.d.ts misses %q", want)
		}
	}
}
