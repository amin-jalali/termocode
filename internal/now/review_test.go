package now

import (
	"reflect"
	"strings"
	"testing"
)

// Spec (Review): the question, its task and the solution come from the
// pull request's text: a "Why"/"Problem"/"Question" part and a
// "What"/"Solution"/"How" part; without them the first paragraph is the
// solution. A task id (ENG-231, #77) in the question is its task.
func TestReviewText(t *testing.T) {
	body := "## Why\nWebhooks fail on short network drops. Fixes ENG-231\n\n## What\nRetry 3 times with backoff. Stop on 4xx.\n\n## Notes\nnone"
	q, task, sol := ReviewText("Add retry to webhook", body)
	if q != "Webhooks fail on short network drops. Fixes ENG-231" || task != "ENG-231" || sol != "Retry 3 times with backoff. Stop on 4xx." {
		t.Errorf("sections: %q %q %q", q, task, sol)
	}
	q, task, sol = ReviewText("Add parser", "Adds a parser.\n\n- one\n- two")
	if q != "" || task != "" || sol != "Adds a parser." {
		t.Errorf("plain body: %q %q %q", q, task, sol)
	}
	q, task, sol = ReviewText("Cache user list", "Question: User list page is slow (2 s). Closes #77\nSolution: Cache the list for 60 s.")
	if q != "User list page is slow (2 s). Closes #77" || task != "#77" || sol != "Cache the list for 60 s." {
		t.Errorf("one-line parts: %q %q %q", q, task, sol)
	}
	if _, _, sol = ReviewText("Bump deps", ""); sol != "Bump deps" {
		t.Errorf("empty body: solution %q, want the title", sol)
	}
	long := strings.Repeat("word ", 100)
	if _, _, sol = ReviewText("x", long); len(sol) > MaxReviewLine {
		t.Errorf("solution is %d bytes, cap %d", len(sol), MaxReviewLine)
	}
}

// Spec (Review): the changes are grouped by meaning, main change first:
// the biggest code change is the main one; tests, config and docs are
// "Rest" with their kinds named.
func TestReviewGroups(t *testing.T) {
	g := ReviewGroups([]ReviewFile{
		{Path: "webhook/send_test.go", Add: 20, Del: 1},
		{Path: "README.md", Add: 2},
		{Path: "webhook/send.go", Add: 18, Del: 2},
		{Path: "config/app.yaml", Add: 3},
		{Path: "webhook/util.go", Add: 1, Del: 1},
	})
	if len(g) != 2 {
		t.Fatalf("groups %+v", g)
	}
	if m := g[0]; !m.Main || m.Title != "send.go" || !reflect.DeepEqual(m.Files, []string{"webhook/send.go"}) || m.Add != 18 || m.Del != 2 {
		t.Errorf("main %+v", m)
	}
	if r := g[1]; r.Main || r.Title != "Rest: code, test, config, docs" || len(r.Files) != 4 || r.Add != 26 || r.Del != 2 {
		t.Errorf("rest %+v", r)
	}
	if g := ReviewGroups([]ReviewFile{{Path: "a_test.go", Add: 3}}); len(g) != 1 || !g[0].Main || g[0].Title != "a_test.go" {
		t.Errorf("tests only %+v", g)
	}
	if g := ReviewGroups(nil); len(g) != 0 {
		t.Errorf("no files %+v", g)
	}
}

// Spec (Review): one proof row per check, each with the commit it ran
// on; a check of an older commit is stale (old code, run it again); a
// skipped one did not run (never "passed"); a red one offers the fix.
func TestReviewProof(t *testing.T) {
	head := "3f2a1c9aa"
	rows := ReviewProof(head, []ProofIn{
		{Name: "Unit tests", State: ProofOK, SHA: head},
		{Name: "e2e-linux", State: ProofFail, SHA: head, URL: "https://x/2"},
		{Name: "Race test", State: ProofOK, SHA: "9b1e0d2bb"},
		{Name: "e2e-macos", State: ProofNotRun, SHA: head},
		{Name: "Lint", State: ProofRunning, SHA: head},
	})
	var got []string
	for _, r := range rows {
		got = append(got, r.Name+":"+r.State+":"+r.SHA)
	}
	want := []string{"e2e-linux:fail:3f2a1c9", "Race test:stale:9b1e0d2", "Lint:running:3f2a1c9", "Unit tests:ok:3f2a1c9", "e2e-macos:notrun:3f2a1c9"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("proof:\n got %v\nwant %v", got, want)
	}
	if !rows[0].Fix || rows[1].Fix || !rows[1].Again {
		t.Errorf("fix / again: %+v", rows[:2])
	}
}

// Spec (Review): risk comes from path rules (higher wins) and size;
// each reason is one plain line.
func TestReviewRisk(t *testing.T) {
	cases := []struct {
		files  []ReviewFile
		level  string
		reason string
	}{
		{[]ReviewFile{{Path: "README.md", Add: 3}}, RiskLow, "docs and tests only"},
		{[]ReviewFile{{Path: "webhook/send.go", Add: 18}, {Path: "net/http_client.go", Add: 1}}, RiskMedium, "touches network code (path rule)"},
		{[]ReviewFile{{Path: "db/migrations/0042_users.sql", Add: 9}}, RiskHigh, "changes a database migration (path rule)"},
		{[]ReviewFile{{Path: "internal/auth/token.go", Add: 2}, {Path: "webhook/send.go", Add: 2}}, RiskHigh, "touches sign-in or secrets (path rule)"},
		{[]ReviewFile{{Path: "a.go", Add: 600}}, RiskMedium, "large change: 600 lines"},
	}
	for _, c := range cases {
		r := ReviewRisk(c.files)
		if r.Level != c.level || len(r.Reasons) == 0 || r.Reasons[0] != c.reason {
			t.Errorf("%v: %+v, want %s %q", c.files, r, c.level, c.reason)
		}
	}
}

// Spec (Review): one short suggestion that names its commit.
func TestReviewSuggestion(t *testing.T) {
	ok := []ProofRow{{Name: "Unit tests", State: ProofOK}}
	if s := ReviewSuggestion(ok); s.Text != "Looks ready to approve." || s.Notes != 0 {
		t.Errorf("all green: %+v", s)
	}
	red := append(ok, ProofRow{Name: "e2e-linux", State: ProofFail})
	if s := ReviewSuggestion(red); s.Text != "Approve after e2e-linux is green." || s.Notes != 1 {
		t.Errorf("red: %+v", s)
	}
	stale := append(ok, ProofRow{Name: "Race test", State: ProofStale})
	if s := ReviewSuggestion(stale); s.Text != "Run Race test again on the new code." || s.Notes != 1 {
		t.Errorf("stale: %+v", s)
	}
}

// Spec (Review): an approval is bound to the commit the user saw: when
// new code came in, it is refused and says both commits.
func TestReviewCanApprove(t *testing.T) {
	if err := CanApprove("3f2a1c9aa", "3f2a1c9aa"); err != nil {
		t.Fatal(err)
	}
	err := CanApprove("3f2a1c9aa", "7d40b21cc")
	if err == nil || err.Error() != "New code came in. You saw 3f2a1c9. Now it is 7d40b21. Approve was not sent." {
		t.Fatalf("head moved: %v", err)
	}
	if CanApprove("", "7d40b21cc") == nil {
		t.Fatal("an approval without the commit it saw was taken")
	}
}
