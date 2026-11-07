package ghci

import (
	"context"
	"strings"
	"testing"
)

const convoJSON = `[
 {"id":901,"user":{"login":"bob"},"body":"Looks fine overall.","created_at":"2026-10-28T12:00:00Z","html_url":"u901"},
 {"id":902,"user":{"login":"ann"},"body":"@termocode please also add a test for the empty input","created_at":"2026-10-28T13:00:00Z","html_url":"u902"},
 {"id":903,"user":null,"body":"cc @agent: rename it","created_at":"2026-10-28T14:00:00Z","html_url":"u903"}
]`

// CMP-31: a mention calls the lane's agent only as a whole word.
func TestMentionsAgent(t *testing.T) {
	for body, want := range map[string]bool{
		"@termocode fix this":     true,
		"please @Agent, rename":   true,
		"(@termocode)":            true,
		"mail me at x@agent.dev":  false,
		"@agents are great":       false,
		"@termocode-bot was here": false,
		"no mention":              false,
	} {
		if got := MentionsAgent(body); got != want {
			t.Errorf("MentionsAgent(%q) = %v, want %v", body, got, want)
		}
	}
}

// IssueComments reads the PR conversation through gh's REST call with
// {owner}/{repo} filled in by gh.
func TestIssueComments(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"api repos/{owner}/{repo}/issues/14/comments?per_page=100": {out: convoJSON}}}
	cs, err := IssueComments(context.Background(), f.run, "/r", 14)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 3 || cs[1].ID != 902 || cs[1].Author != "ann" || cs[2].Author != "ghost" || cs[0].URL != "u901" {
		t.Fatalf("comments %+v", cs)
	}
	if _, err := IssueComments(context.Background(), f.run, "/r", 0); err == nil {
		t.Error("no PR number must fail")
	}
}

func followThreads() []Thread {
	return []Thread{
		{ID: "T1", Path: "parser.go", Line: 12, Comments: []ThreadComment{{ID: 501, Author: "bob", Body: "Why a map?"}, {ID: 502, Author: "ann", Body: "token=ghp_abcdefghijklmnopqrstuvwxyz0123456789"}}},
		{ID: "T2", Path: "a.md", Line: 3, Resolved: true, Comments: []ThreadComment{{ID: 600, Author: "bob", Body: "typo"}}},
		{ID: "T3", Path: "b.go", Line: 7, Comments: []ThreadComment{{ID: 700, Author: "bob", Body: "old one"}}},
	}
}

// NewFeedback keeps open threads with an unseen comment, mentions in
// the conversation that are not seen and a failed run not sent yet.
func TestNewFeedback(t *testing.T) {
	convo := []ThreadComment{{ID: 901, Body: "Looks fine"}, {ID: 902, Author: "ann", Body: "@termocode add a test"}}
	snap := Snapshot{Branch: "feat/x", Runs: []Run{{ID: 30, Workflow: "CI", Status: "completed", Conclusion: "failure"}}}
	seen := map[int64]bool{700: true}
	f := NewFeedback("feat/x", 14, followThreads(), convo, snap, seen, 0)
	if len(f.Threads) != 1 || f.Threads[0].ID != "T1" {
		t.Errorf("threads %+v (resolved and seen ones must go)", f.Threads)
	}
	if len(f.Mentions) != 1 || f.Mentions[0].ID != 902 {
		t.Errorf("mentions %+v", f.Mentions)
	}
	if f.Run == nil || f.Run.ID != 30 {
		t.Errorf("run %+v", f.Run)
	}
	if f.Empty() {
		t.Error("not empty")
	}
	if got := f.Summary(); got != "2 review comments · 1 mention · CI failed (CI)" {
		t.Errorf("summary %q", got)
	}
	ids := f.SeenIDs()
	if len(ids) != 3 || ids[0] != 501 || ids[1] != 502 || ids[2] != 902 {
		t.Errorf("seen ids %v", ids)
	}
	// Everything seen and the run sent: nothing new.
	all := map[int64]bool{501: true, 502: true, 700: true, 902: true}
	if f2 := NewFeedback("feat/x", 14, followThreads(), convo, snap, all, 30); !f2.Empty() {
		t.Errorf("want empty, got %+v", f2)
	}
	// A green or pending run is not feedback.
	snap.Runs[0].Conclusion = "success"
	if f3 := NewFeedback("feat/x", 14, nil, nil, snap, nil, 0); !f3.Empty() {
		t.Errorf("green CI is feedback: %+v", f3)
	}
}

// Prompt is the file the agent reads: each thread with its place and
// comments, the mentions, the failing log; secrets are redacted.
func TestFeedbackPrompt(t *testing.T) {
	snap := Snapshot{Runs: []Run{{ID: 30, Workflow: "CI", Status: "completed", Conclusion: "failure"}}}
	f := NewFeedback("feat/x", 14, followThreads(), []ThreadComment{{ID: 902, Author: "ann", Body: "@termocode add a test"}}, snap, nil, 0)
	f.Log = FailedLog{Job: "test", Step: "go test", Lines: []string{"--- FAIL: TestX"}, Total: 1}
	p := f.Prompt()
	for _, want := range []string{"PR #14", "feat/x", "parser.go:12", "bob (new): Why a map?", "@termocode add a test", "Job: test", "--- FAIL: TestX", "b.go:7"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt misses %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, "ghp_abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Errorf("secret not redacted:\n%s", p)
	}
	if strings.Contains(p, "a.md:3") {
		t.Errorf("resolved thread in the prompt:\n%s", p)
	}
	line := AgentLine("/g/termocode/feedback/x-pr-14.md", f)
	if strings.ContainsAny(line, "\n\r") || !strings.Contains(line, "/g/termocode/feedback/x-pr-14.md") || !strings.Contains(line, "PR #14") {
		t.Errorf("agent line %q", line)
	}
}

// FetchFeedback reads threads, the conversation and CI; the failing log
// only when the run is new.
func TestFetchFeedback(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{
		"api graphql": {out: threadsJSON},
		"api repos/{owner}/{repo}/issues/14/comments?per_page=100": {out: convoJSON},
		"run list": {out: runsJSON},
		"pr view":  {out: prJSONText},
		"run view": {out: "test\tgo test\t2026-10-28T10:00:00Z --- FAIL: TestX\n"},
	}}
	fb, err := FetchFeedback(context.Background(), f.run, "/r", "feat/x", 14, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(fb.Threads) != 1 || len(fb.Mentions) != 2 || fb.Run == nil || fb.Run.ID != 30 || fb.Log.Job != "test" {
		t.Fatalf("feedback %+v", fb)
	}
	f.calls = nil
	fb, err = FetchFeedback(context.Background(), f.run, "/r", "feat/x", 14, nil, 30)
	if err != nil {
		t.Fatal(err)
	}
	if fb.Run != nil {
		t.Errorf("run already sent: %+v", fb.Run)
	}
	for _, c := range f.calls {
		if strings.Join(c[:2], " ") == "run view" {
			t.Error("log read for a run already sent")
		}
	}
}
