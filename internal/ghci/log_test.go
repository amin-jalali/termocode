package ghci

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func logLine(job, step, text string) string {
	return job + "\t" + step + "\t2026-10-29T08:00:01.1234567Z " + text
}

// The parser strips ANSI and timestamps, keeps the first job, names the
// step of the first error and lists the other failed jobs.
func TestParseFailedLog(t *testing.T) {
	out := strings.Join([]string{
		logLine("test", "Set up job", "Runner image ok"),
		logLine("test", "Run tests", "\x1b[31m--- FAIL: TestParse (0.00s)\x1b[0m"),
		logLine("test", "Run tests", "    parse_test.go:12: got 1 want 2"),
		logLine("test", "Run tests", "##[endgroup]"),
		logLine("lint", "golangci", "error: unused var"),
		"not a log line",
	}, "\n")
	fl := ParseFailedLog(out)
	if fl.Job != "test" || fl.Step != "Run tests" {
		t.Errorf("job %q step %q", fl.Job, fl.Step)
	}
	if len(fl.OtherJobs) != 1 || fl.OtherJobs[0] != "lint" {
		t.Errorf("other jobs %v", fl.OtherJobs)
	}
	want := []string{"Runner image ok", "--- FAIL: TestParse (0.00s)", "    parse_test.go:12: got 1 want 2"}
	if strings.Join(fl.Lines, "|") != strings.Join(want, "|") || fl.Total != 3 || len(fl.All) != 3 {
		t.Errorf("lines %q total %d", fl.Lines, fl.Total)
	}
}

// A long log keeps MaxLogLines: from a little before the first error,
// else the end.
func TestTrimLog(t *testing.T) {
	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	got := TrimLog(lines, 200)
	if len(got) != 200 || got[199] != "line 499" {
		t.Errorf("no error: %d lines, last %q", len(got), got[len(got)-1])
	}
	lines[100] = "##[error]Process completed with exit code 1."
	got = TrimLog(lines, 200)
	if len(got) != 200 || got[0] != "line 80" || got[20] != lines[100] {
		t.Errorf("error window starts %q, [20]=%q", got[0], got[20])
	}
	lines[100] = "line 100"
	lines[490] = "panic: boom"
	got = TrimLog(lines, 200)
	if len(got) != 200 || got[199] != "line 499" {
		t.Errorf("late error: last %q", got[len(got)-1])
	}
	if got := TrimLog(lines[:5], 200); len(got) != 5 {
		t.Errorf("short log cut to %d", len(got))
	}
}

// The prompt names the branch, job and step, holds the log with secrets
// redacted, and ends with the request; it is never more than the cap.
func TestFixPrompt(t *testing.T) {
	tok := "ghp_" + "abcdefghijklmnopqrstuvwxyz0123"
	fl := FailedLog{Job: "test", Step: "Run tests", OtherJobs: []string{"lint"},
		Lines: []string{"--- FAIL: TestX", "token=" + tok}, Total: 900}
	p := FixPrompt("feat/x", Run{Workflow: "CI", URL: "https://github.com/o/r/actions/runs/30"}, fl)
	for _, want := range []string{"CI failed on branch feat/x (workflow CI).", "Job: test", "Step: Run tests",
		"Other failed jobs: lint", "Run: https://github.com/o/r/actions/runs/30", "(2 of 900 lines)", "--- FAIL: TestX",
		"```text", "Find the cause and fix it."} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt misses %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, tok) || !strings.Contains(p, "[REDACTED]") {
		t.Errorf("token not redacted:\n%s", p)
	}
	if !strings.HasSuffix(p, "Find the cause and fix it.") {
		t.Error("request is not last")
	}
}

func TestFetchFailedLog(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"run view": {out: logLine("test", "Run tests", "FAIL x")}}}
	fl, err := FetchFailedLog(context.Background(), f.run, "/r", 30)
	if err != nil || fl.Job != "test" {
		t.Fatalf("%+v %v", fl, err)
	}
	if got := strings.Join(f.calls[0], " "); got != "run view 30 --log-failed" {
		t.Errorf("args %q", got)
	}
	if _, err := FetchFailedLog(context.Background(), f.run, "/r", 0); err == nil {
		t.Error("run 0 accepted")
	}
}
