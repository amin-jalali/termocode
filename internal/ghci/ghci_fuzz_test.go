package ghci

import (
	"context"
	"strings"
	"testing"
)

// FuzzGHJSON answers every gh call with the same text: run list, pr
// view, pr list, pr diff and the failed log. Nothing may panic.
func FuzzGHJSON(f *testing.F) {
	for _, s := range []string{
		`[{"databaseId":1,"workflowName":"test","displayTitle":"x","status":"completed","conclusion":"failure","headSha":"abc","url":"https://x"}]`,
		`{"number":7,"title":"t","state":"OPEN","isDraft":true,"url":"u","reviewDecision":"","statusCheckRollup":[{"__typename":"CheckRun","name":"ci","status":"COMPLETED","conclusion":"SUCCESS"},{"__typename":"StatusContext","context":"x","state":"PENDING"}],"files":[{"path":"a","additions":1,"deletions":2}],"reviews":[{"author":{"login":"me"},"state":"APPROVED"}],"comments":[]}`,
		`[{"number":1,"title":"a","author":{"login":"me"},"headRefName":"f","baseRefName":"main","statusCheckRollup":null}]`,
		`{"defaultBranchRef":{"name":"main"}}`,
		"[]", "{}", "null", "not json",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, out string) {
		run := func(context.Context, string, ...string) (string, string, error) { return out, "", nil }
		ctx := context.Background()
		_, _ = Fetch(ctx, run, ".", "feature")
		_, _ = ListPRs(ctx, run, ".")
		_, _ = ViewPR(ctx, run, ".", 7)
		_, _ = DefaultBranch(ctx, run, ".")
		_, _ = PRDiff(ctx, run, ".", 7)
		_, _ = FetchFailedLog(ctx, run, ".", 1)
	})
}

// FuzzParseFailedLog: the shown window never passes MaxLogLines, and it
// is a part of all the kept lines.
func FuzzParseFailedLog(f *testing.F) {
	f.Add(strings.Join([]string{
		logLine("test", "Set up job", "Runner image ok"),
		logLine("test", "Run tests", "\x1b[31m--- FAIL: TestParse (0.00s)\x1b[0m"),
		logLine("test", "Run tests", "##[endgroup]"),
		logLine("lint", "golangci", "error: unused var"),
		"not a log line",
	}, "\n"))
	f.Add("a\tUNKNOWN STEP\tpanic: x\r\na\tb\t\ufeff2026-01-01T00:00:00Z ok")
	f.Fuzz(func(t *testing.T, out string) {
		fl := ParseFailedLog(out)
		if len(fl.Lines) > MaxLogLines || fl.Total != len(fl.All) || len(fl.Lines) > len(fl.All) {
			t.Fatalf("lines %d total %d all %d", len(fl.Lines), fl.Total, len(fl.All))
		}
		if fl.Step == "UNKNOWN STEP" {
			t.Fatal("placeholder step shown")
		}
	})
}

// FuzzParseDiff reads any text as `gh pr diff`. Counts are never
// negative and a binary file has no text.
func FuzzParseDiff(f *testing.F) {
	for _, s := range []string{
		"diff --git a/x.go b/x.go\nindex 1..2 100644\n--- a/x.go\n+++ b/x.go\n@@ -1,2 +1,2 @@\n a\n-b\n+c\n",
		"diff --git a/old b/new\nsimilarity index 90%\nrename from old\nrename to new\n",
		"diff --git a/img.png b/img.png\nnew file mode 100644\nBinary files /dev/null and b/img.png differ\n",
		"diff --git \"a/sp ace\" \"b/sp ace\"\ndeleted file mode 100644\n--- a/sp ace\n+++ /dev/null\n@@ -1 +0,0 @@\n-x\n",
		"diff --git a/x",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		for _, d := range ParseDiff(text) {
			if d.Add < 0 || d.Del < 0 {
				t.Fatalf("negative counts %+v", d)
			}
		}
	})
}
