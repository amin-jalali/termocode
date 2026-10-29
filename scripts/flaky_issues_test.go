package scripts

// Tests for flaky-issues.sh with a fake gh on PATH: no network.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// goTestJSON is `go test -json -count=2` output where TestA fails once
// and TestB fails once.
const goTestJSON = `{"Action":"run","Package":"p/x","Test":"TestA"}
{"Action":"output","Package":"p/x","Test":"TestA","Output":"    a_test.go:9: boom A\n"}
{"Action":"fail","Package":"p/x","Test":"TestA"}
{"Action":"run","Package":"p/x","Test":"TestA"}
{"Action":"pass","Package":"p/x","Test":"TestA"}
{"Action":"run","Package":"p/x","Test":"TestB"}
{"Action":"output","Package":"p/x","Test":"TestB","Output":"    b_test.go:3: boom B\n"}
{"Action":"fail","Package":"p/x","Test":"TestB"}
{"Action":"fail","Package":"p/x"}
`

// fakeGH writes a gh that logs each call (and the body file) to log and
// reads all its stdin, like a tool that might. "issue list" prints
// openNum when set.
func runFlakyIssues(t *testing.T, openNum string) string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "gh.log")
	gh := `#!/usr/bin/env bash
cat > /dev/null
echo "gh $*" >> "` + log + `"
prev=""
for a in "$@"; do
	if [ "$prev" = "--body-file" ]; then cat "$a" >> "` + log + `"; fi
	prev=$a
done
if [ "$1 $2" = "issue list" ]; then echo "` + openNum + `"; fi
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(gh), 0o755); err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(dir, "out.json")
	if err := os.WriteFile(in, []byte(goTestJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "scripts/flaky-issues.sh", in, "seed 4242")
	cmd.Dir = ".."
	cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GITHUB_REPOSITORY=o/r", "GITHUB_RUN_ID=99")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("flaky-issues.sh: %v\n%s", err, out)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("gh never called: %v", err)
	}
	return string(b)
}

// Spec: with no open issue, each failed test gets a new issue titled
// "Flaky: <TestName>" whose body has the run link, the note (seed), how
// many runs failed and the failing log tail.
func TestFlakyIssuesCreatesOnePerTest(t *testing.T) {
	log := runFlakyIssues(t, "")
	for _, want := range []string{
		`gh issue create --title Flaky: TestA --body-file`,
		`gh issue create --title Flaky: TestB --body-file`,
		"seed 4242", "boom A", "boom B", "failed 1/2 runs", "https://github.com/o/r/actions/runs/99",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("gh log lacks %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "issue comment") {
		t.Errorf("commented with no open issue:\n%s", log)
	}
}

// Spec: an open issue with the same title gets a comment, no new issue.
func TestFlakyIssuesCommentsOnOpenIssue(t *testing.T) {
	log := runFlakyIssues(t, "7")
	if !strings.Contains(log, "gh issue comment 7 --body-file") || strings.Contains(log, "issue create") {
		t.Errorf("want a comment on #7 and no new issue:\n%s", log)
	}
	if n := strings.Count(log, "gh issue comment 7"); n != 2 {
		t.Errorf("%d comments, want 2 (TestA, TestB):\n%s", n, log)
	}
}
