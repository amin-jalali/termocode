//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// E2E-GIT: the pull request list, a pull request's card and the merge
// card on the real binary. gh is a fake script on a temp PATH (never the
// real gh, never GitHub); it logs its arguments.

const prFakeGH = `#!/bin/sh
echo "$@" >> "$PR_GH_LOG"
case "$1 $2" in
"pr list")
	echo '[{"number":14,"title":"Add parser","author":{"login":"ann"},"headRefName":"feat/parser","baseRefName":"main","url":"https://example.invalid/pull/14","isDraft":false,"state":"OPEN","reviewDecision":"APPROVED","statusCheckRollup":[{"name":"test","status":"COMPLETED","conclusion":"SUCCESS"}]}]'
	exit 0 ;;
"pr view")
	if [ "$3" = "14" ]; then
	echo '{"number":14,"title":"Add parser","body":"Adds a parser.","author":{"login":"ann"},"headRefName":"feat/parser","headRefOid":"abc1234","baseRefName":"main","state":"OPEN","isDraft":false,"url":"https://example.invalid/pull/14","reviewDecision":"APPROVED","mergeable":"MERGEABLE","statusCheckRollup":[{"name":"test","status":"COMPLETED","conclusion":"SUCCESS"}],"files":[{"path":"parser.go","additions":40,"deletions":2}],"reviews":[],"comments":[]}'
	exit 0
	fi
	echo 'no pull requests found' >&2
	exit 1 ;;
"pr merge")
	exit 0 ;;
esac
echo "fake gh: unexpected $*" >&2
exit 1
`

func startPR(t *testing.T) (*Term, string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(prFakeGH), 0o755); err != nil {
		t.Fatal(err)
	}
	ghLog := filepath.Join(t.TempDir(), "gh.log")
	env := []string{"PR_GH_LOG=" + ghLog, "PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")}
	tm := startWindow(t, t.TempDir(), NewRepo(t), env)
	tm.settle()
	tm.Palette("Git: Pull Requests…")
	tm.WaitText("Add parser", uiTimeout)
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("Actions…", uiTimeout)
	tm.settle()
	tm.ClickText("Actions…")
	tm.WaitText("Merge…", uiTimeout)
	tm.settle()
	tm.Type("Merge")
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("Squash and merge", uiTimeout)
	tm.settle()
	return tm, ghLog
}

func mergesIn(log string) int { return strings.Count(fileText(log), "pr merge") }

// Spec (GITX PR): the list shows the pull request; Enter opens its card;
// Actions… then Merge… opens the merge card (Cancel first). Esc merges
// nothing.
func TestGitPRMergeCardEscMergesNothing(t *testing.T) {
	tm, ghLog := startPR(t)
	if !tm.Has("All checks passed.") {
		t.Errorf("the merge card does not show the checks:\n%s", tm.Screen())
	}
	tm.Send(KeyEsc)
	tm.WaitGone("Squash and merge", uiTimeout)
	time.Sleep(500 * time.Millisecond)
	if n := mergesIn(ghLog); n != 0 {
		t.Fatalf("Esc ran gh pr merge (%d times):\n%s", n, fileText(ghLog))
	}
}

// Spec: Squash and merge runs `gh pr merge 14 --squash` once (never
// --admin, --auto or --delete-branch) and says "Merged #14".
func TestGitPRSquashMerge(t *testing.T) {
	tm, ghLog := startPR(t)
	tm.ClickText("Squash and merge")
	waitUntil(t, "gh pr merge", 15*time.Second, func() bool { return mergesIn(ghLog) == 1 })
	if !strings.Contains(fileText(ghLog), "pr merge 14 --squash") {
		t.Errorf("gh got:\n%s", fileText(ghLog))
	}
	for _, bad := range []string{"--admin", "--auto", "--delete-branch"} {
		if strings.Contains(fileText(ghLog), bad) {
			t.Errorf("gh got %s", bad)
		}
	}
	tm.WaitText("Merged #14", uiTimeout)
}
