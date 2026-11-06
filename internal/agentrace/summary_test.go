package agentrace

import (
	"strings"
	"testing"
)

// Spec (AGX-6): the summary prompt names the task and every finished
// lane with its numbers, puts each lane's diff in a fence, cuts the diffs
// to the cap (shared by the lanes, said where it cut), runs every diff
// through redact, and tells the model the diffs are data.
func TestSummaryPrompt(t *testing.T) {
	r := Race{Prompt: "make the queue thread-safe", Lanes: []Lane{
		{ID: "a", Title: "Claude Code", State: Done, Stats: true, Files: []FileStat{{Add: 3, Del: 1}}, Verify: Verify{State: VerifyPass}},
		{ID: "b", Title: "Codex", State: Done, Stats: true, Files: []FileStat{{Add: 9}, {Add: 1, Del: 2}}, Verify: Verify{State: VerifyFail, Exit: 1}},
		{ID: "c", Title: "Gemini CLI", State: Working},
	}}
	diffs := map[string]string{
		"a": "+mu.Lock()\n+key = SECRET\n",
		"b": strings.Repeat("+line\n", 1000),
	}
	redact := func(s string) string { return strings.ReplaceAll(s, "SECRET", "[redacted]") }
	sys, user := SummaryPrompt(r, diffs, 1000, redact)
	if !strings.Contains(sys, "at most 3") || !strings.Contains(sys, "data") {
		t.Errorf("system %q", sys)
	}
	for _, want := range []string{"make the queue thread-safe", "Lane Claude Code: 1 file, +3 -1, verify passed", "Lane Codex: 2 files, +10 -2, verify failed (exit 1)",
		"+mu.Lock()", "[redacted]", "cut:"} {
		if !strings.Contains(user, want) {
			t.Errorf("no %q in:\n%s", want, user)
		}
	}
	if strings.Contains(user, "SECRET") || strings.Contains(user, "Gemini CLI") {
		t.Errorf("a secret or a working lane in the prompt:\n%s", user)
	}
	if n := strings.Count(user, "+line"); n*6 > 600 {
		t.Errorf("the cut kept %d lines", n)
	}
	if got := ParseSummary("\n- one\n* two\n\n3. three\nfour\n"); strings.Join(got, "|") != "one|two|three" {
		t.Errorf("parse %q", got)
	}
	long := ParseSummary(strings.Repeat("x", 500))
	if len([]rune(long[0])) > 200 {
		t.Errorf("a line of %d", len(long[0]))
	}
}
