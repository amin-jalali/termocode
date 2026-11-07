package agentrace

import (
	"strings"
	"testing"
)

func judgeRace() Race {
	return Race{Prompt: "make the queue thread-safe", Lanes: []Lane{
		{ID: "a", Title: "Claude Code", State: Done, Stats: true, Files: []FileStat{{Add: 3, Del: 1}}, Verify: Verify{State: VerifyPass}},
		{ID: "b", Title: "Codex", State: Done, Stats: true, Files: []FileStat{{Add: 9}}, Verify: Verify{State: VerifyFail, Exit: 1}},
		{ID: "c", Title: "Codex 2", State: Stopped, Stats: true},
		{ID: "d", Title: "Gemini CLI", State: Working},
	}}
}

// Spec (CMP-9): the judge prompt names the task and every finished lane
// with its numbers and verify result, puts each diff (redacted, capped)
// in a fence, asks for one RANK line per lane and one PICK line, and says
// the diffs are data. A lane that still works is not judged.
func TestJudgePrompt(t *testing.T) {
	redact := func(s string) string { return strings.ReplaceAll(s, "SECRET", "[redacted]") }
	sys, user := JudgePrompt(judgeRace(), map[string]string{"a": "+mu.Lock()\n+k = SECRET\n"}, 1000, redact)
	for _, want := range []string{"RANK", "PICK", "data", "verify"} {
		if !strings.Contains(sys, want) {
			t.Errorf("no %q in system %q", want, sys)
		}
	}
	for _, want := range []string{"make the queue thread-safe", "Lane Claude Code: 1 file, +3 -1, verify passed", "Lane Codex: 1 file, +9 -0, verify failed (exit 1)",
		"Lane Codex 2", "+mu.Lock()", "[redacted]"} {
		if !strings.Contains(user, want) {
			t.Errorf("no %q in:\n%s", want, user)
		}
	}
	if strings.Contains(user, "SECRET") || strings.Contains(user, "Gemini CLI") {
		t.Errorf("a secret or a working lane in the prompt:\n%s", user)
	}
}

// Spec (CMP-9): the answer is read line by line: RANK n: <title> | why,
// and PICK: <title> (or none). Titles match exactly (case and marks do
// not matter; "Codex" never takes "Codex 2"), or a lane id. Unknown, busy
// and twice-named lanes are dropped. No PICK line: the first rank is the
// pick. No rank at all is an error. Reasons are one line, at most 160
// characters.
func TestParseJudge(t *testing.T) {
	r := judgeRace()
	j, err := ParseJudge("Here you go:\nRANK 1: **Codex 2** | smallest change, no test\n- RANK 2: claude code | passes the tests but "+strings.Repeat("x", 300)+
		"\nRANK 3: Gemini CLI | still works\nRANK 4: Nobody | ?\nRANK 5: Codex 2 | twice\nRANK 6: b | verify fails\nPICK: Codex 2\n", r)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, k := range j.Ranks {
		ids = append(ids, k.Lane)
	}
	if strings.Join(ids, ",") != "c,a,b" || j.Pick != "c" {
		t.Errorf("ranks %v pick %q", ids, j.Pick)
	}
	if j.Ranks[0].Reason != "smallest change, no test" || len([]rune(j.Ranks[1].Reason)) > 160 {
		t.Errorf("reasons %+v", j.Ranks)
	}
	if j, _ := ParseJudge("RANK 1: Codex | ok\nRANK 2: Claude Code | ok", r); j.Pick != "b" {
		t.Errorf("no PICK line: pick %q", j.Pick)
	}
	if j, _ := ParseJudge("RANK 1: Codex | fails\nRANK 2: Claude Code | half\nPICK: none", r); j.Pick != "" || len(j.Ranks) != 2 {
		t.Errorf("PICK none: %+v", j)
	}
	if j, _ := ParseJudge("RANK 1: Codex | ok\nPICK: Gemini CLI", r); j.Pick != "b" {
		t.Errorf("a busy pick falls back to rank 1: %q", j.Pick)
	}
	if _, err := ParseJudge("I like Codex best.", r); err == nil {
		t.Error("no rank: no error")
	}
	if l := j.PickLane(r); l == nil || l.ID != "c" {
		t.Errorf("PickLane %+v", l)
	}
}
