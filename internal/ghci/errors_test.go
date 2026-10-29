package ghci

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// gh answered, but not with the JSON we asked for (an old gh, a proxy
// page): every reader says which command broke instead of showing junk.
func TestMalformedJSONFromGH(t *testing.T) {
	ctx := context.Background()
	bad := fakeAnswer{out: "<html>proxy</html>"}

	f := &fakeGH{answers: map[string]fakeAnswer{"run list": bad}}
	if _, err := Fetch(ctx, f.run, "/r", "x"); err == nil || !strings.Contains(err.Error(), "gh run list") {
		t.Errorf("Fetch with bad run JSON = %v", err)
	}
	f = &fakeGH{answers: map[string]fakeAnswer{"run list": {out: "[]"}, "pr view": bad}}
	if _, err := Fetch(ctx, f.run, "/r", "x"); err == nil || !strings.Contains(err.Error(), "gh pr view") {
		t.Errorf("Fetch with bad PR JSON = %v", err)
	}
	f = &fakeGH{answers: map[string]fakeAnswer{"pr view": bad}}
	if _, err := ViewPR(ctx, f.run, "/r", 3); err == nil || !strings.Contains(err.Error(), "gh pr view") {
		t.Errorf("ViewPR with bad JSON = %v", err)
	}
	f = &fakeGH{answers: map[string]fakeAnswer{"pr list": bad}}
	if _, err := ListPRs(ctx, f.run, "/r"); err == nil || !strings.Contains(err.Error(), "gh pr list") {
		t.Errorf("ListPRs with bad JSON = %v", err)
	}
	f = &fakeGH{answers: map[string]fakeAnswer{"repo view": bad}}
	if _, err := DefaultBranch(ctx, f.run, "/r"); err == nil {
		t.Error("DefaultBranch with bad JSON: want error")
	}
}

// Every command reports gh not being signed in as the quiet error.
func TestNotLoggedInEverywhere(t *testing.T) {
	ctx := context.Background()
	no := fakeAnswer{errOut: "To get started with GitHub CLI, please run:  gh auth login", err: errors.New("exit status 4")}
	all := map[string]fakeAnswer{"run list": no, "run view": no, "pr view": no, "pr list": no, "pr diff": no, "pr create": no, "repo view": no}
	f := &fakeGH{answers: all}
	checks := map[string]error{}
	_, checks["Fetch"] = Fetch(ctx, f.run, "/r", "x")
	_, checks["FetchFailedLog"] = FetchFailedLog(ctx, f.run, "/r", 7)
	_, checks["ViewPR"] = ViewPR(ctx, f.run, "/r", 1)
	_, checks["ListPRs"] = ListPRs(ctx, f.run, "/r")
	_, checks["PRDiff"] = PRDiff(ctx, f.run, "/r", 1)
	_, checks["Create"] = Create(ctx, f.run, "/r", NewPR{Title: "t", Branch: "b"})
	_, checks["DefaultBranch"] = DefaultBranch(ctx, f.run, "/r")
	for name, err := range checks {
		if !errors.Is(err, ErrNotLoggedIn) || !Quiet(err) {
			t.Errorf("%s = %v, want ErrNotLoggedIn", name, err)
		}
	}
}

// A PR view failure after the runs were read keeps the runs.
func TestFetchKeepsRunsWhenPRFails(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{
		"run list": {out: runsJSON},
		"pr view":  {errOut: "HTTP 502: Bad gateway", err: errors.New("exit status 1")},
	}}
	s, err := Fetch(context.Background(), f.run, "/r", "x")
	if err == nil || err.Error() != "gh: HTTP 502: Bad gateway" {
		t.Errorf("err = %v", err)
	}
	if s.HeadSHA != "bbb" || len(s.Runs) != 2 {
		t.Errorf("runs lost: %+v", s)
	}
}

func TestClassifySilentFailure(t *testing.T) {
	err := classify(errors.New("exit status 2"), "  \n")
	if err == nil || err.Error() != "gh: exit status 2" || Quiet(err) {
		t.Errorf("classify with no stderr = %v", err)
	}
	if classify(nil, "anything") != nil {
		t.Error("classify(nil) must be nil")
	}
}

// Commit statuses (the older API) count as pass / fail / pending too.
func TestStatusContextStates(t *testing.T) {
	cs := checksOf([]rollupJSON{
		{Typename: "StatusContext", Context: "ok", State: "SUCCESS", TargetURL: "u1"},
		{Typename: "StatusContext", Context: "bad", State: "FAILURE"},
		{Typename: "StatusContext", Context: "err", State: "error"},
		{Typename: "StatusContext", Context: "wait", State: "EXPECTED"},
	})
	want := []State{StatePass, StateFail, StateFail, StatePending}
	for i, c := range cs {
		if c.State != want[i] {
			t.Errorf("%s: %v, want %v", c.Name, c.State, want[i])
		}
	}
	if cs[0].Name != "ok" || cs[0].URL != "u1" {
		t.Errorf("status context name/url = %+v", cs[0])
	}
	if got := sumChecks([]Check{{State: StatePass}, {State: StatePending}}); got != StatePending {
		t.Errorf("pass + pending = %v, want pending", got)
	}
}

func TestParseFailedLogStepFallbacks(t *testing.T) {
	// No line looks like an error: the step is the last one seen.
	fl := ParseFailedLog(logLine("build", "Compile", "ok") + "\n" + logLine("build", "Upload", "done"))
	if fl.Step != "Upload" {
		t.Errorf("step = %q, want the last step", fl.Step)
	}
	// gh could not name the step.
	fl = ParseFailedLog(logLine("build", "UNKNOWN STEP", "all good"))
	if fl.Step != "" {
		t.Errorf("step = %q, want none", fl.Step)
	}
	// Another job failing twice is named once.
	fl = ParseFailedLog(strings.Join([]string{
		logLine("a", "s", "error: x"), logLine("b", "s", "error: y"), logLine("b", "s", "error: z"),
	}, "\n"))
	if strings.Join(fl.OtherJobs, ",") != "b" {
		t.Errorf("other jobs = %v", fl.OtherJobs)
	}
}

func TestTrimLogErrorNearTop(t *testing.T) {
	var lines []string
	for i := 0; i < MaxLogLines+50; i++ {
		lines = append(lines, "ok")
	}
	lines[3] = "panic: boom"
	got := TrimLog(lines, MaxLogLines)
	if len(got) != MaxLogLines || got[3] != "panic: boom" {
		t.Errorf("window does not start at the top: len %d, line 3 %q", len(got), got[3])
	}
}

func TestFixPromptWholeLog(t *testing.T) {
	fl := FailedLog{Job: "test", Lines: []string{"--- FAIL: TestX"}, Total: 1}
	p := FixPrompt("main", Run{}, fl)
	if !strings.Contains(p, "\nFailing log:\n") || strings.Contains(p, "of 1 lines") || strings.Contains(p, "workflow") {
		t.Errorf("prompt:\n%s", p)
	}
}

func TestFetchFailedLogNoRun(t *testing.T) {
	f := &fakeGH{}
	if _, err := FetchFailedLog(context.Background(), f.run, "/r", 0); err == nil || len(f.calls) != 0 {
		t.Errorf("no run id: err %v calls %v", err, f.calls)
	}
}

// Diffs made with diff.noprefix, odd headers and blank context lines.
func TestParseDiffOddHeaders(t *testing.T) {
	text := "diff --git x.go x.go\n--- x.go\n+++ x.go\n@@ -1,3 +1,3 @@\n a\n\n-b\n+B\n" +
		"diff --git weird\n@@ -1 +1 @@\n-1\n+2\n"
	files := ParseDiff(text)
	if len(files) != 2 {
		t.Fatalf("files = %+v", files)
	}
	if files[0].Path != "x.go" || files[0].OldPath != "x.go" || files[0].Old != "a\n\nb\n" || files[0].New != "a\n\nB\n" {
		t.Errorf("noprefix file = %+v", files[0])
	}
	if files[1].Path != "weird" || files[1].Add != 1 {
		t.Errorf("odd header file = %+v", files[1])
	}
}

func TestCreateWithoutURLInOutput(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"pr create": {out: "Creating pull request...\n"}}}
	url, err := Create(context.Background(), f.run, "/r", NewPR{Title: "t", Branch: "b"})
	if err != nil || url != "" {
		t.Errorf("Create = %q, %v; want no URL and no error", url, err)
	}
}
