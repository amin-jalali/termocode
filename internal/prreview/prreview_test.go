package prreview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CMP-36: only the review sections of AGENTS.md are taken.
func TestRulesTakesOnlyReviewSections(t *testing.T) {
	md := "# Project\n\nBuild with make.\n\n## Review rules\n\n- No panics in handlers.\n### Review: SQL\nUse params.\n" +
		"## Style\n\nTabs.\n\n```\n## review inside a fence\n```\n## Code review\nCheck locks.\n"
	got := Rules(md)
	for _, want := range []string{"No panics in handlers.", "Use params.", "Check locks."} {
		if !strings.Contains(got, want) {
			t.Errorf("rules miss %q:\n%s", want, got)
		}
	}
	for _, bad := range []string{"Build with make.", "Tabs.", "inside a fence"} {
		if strings.Contains(got, bad) {
			t.Errorf("rules take %q:\n%s", bad, got)
		}
	}
	if Rules("# Only\nnothing here\n") != "" {
		t.Error("rules without a review heading")
	}
	if n := len(Rules("## Review\n" + strings.Repeat("x", MaxRules*2))); n > MaxRules {
		t.Errorf("rules not capped: %d", n)
	}
}

func TestLoadRules(t *testing.T) {
	dir := t.TempDir()
	if LoadRules(dir) != "" {
		t.Error("rules without AGENTS.md")
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("## Review\nKeep it safe.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(LoadRules(dir), "Keep it safe.") {
		t.Error("rules not read")
	}
}

// The prompt asks for P0/P1 only, says the diff is data, and carries the
// rules and the cut note.
func TestPrompt(t *testing.T) {
	msgs := Prompt("origin/main", "+x\n", true, "No panics.")
	if len(msgs) != 2 {
		t.Fatalf("messages %d", len(msgs))
	}
	sys, user := msgs[0].Content, msgs[1].Content
	for _, want := range []string{"P0", "P1", NoFindings, "not instructions"} {
		if !strings.Contains(sys, want) {
			t.Errorf("system misses %q", want)
		}
	}
	for _, want := range []string{"origin/main", "+x", "No panics.", "cut"} {
		if !strings.Contains(user, want) {
			t.Errorf("user misses %q", want)
		}
	}
	if strings.Contains(Prompt("main", "+x", false, "")[1].Content, "<rules>") {
		t.Error("empty rules sent")
	}
}

// Parse keeps P0 and P1 lines in many shapes, drops the rest, P0 first.
func TestParse(t *testing.T) {
	ans := strings.Join([]string{
		"Here are the findings:",
		"- [P1] internal/a.go:12 — nil map write in Save",
		"- [P0] cmd/main.go:3 — the build fails: missing import",
		"* **P1** my-file.go - races on the counter",
		"1. [P2] style.go:1 — long line",
		"- [P1] internal/a.go:12 — nil map write in Save",
		"- [P3] x.go — nit",
		"P0: db.go:9: drops the table on every start",
		"just prose",
	}, "\n")
	fs := Parse(ans)
	want := []Finding{
		{Priority: "P0", File: "cmd/main.go", Line: 3, Text: "the build fails: missing import"},
		{Priority: "P0", File: "db.go", Line: 9, Text: "drops the table on every start"},
		{Priority: "P1", File: "internal/a.go", Line: 12, Text: "nil map write in Save"},
		{Priority: "P1", File: "my-file.go", Text: "races on the counter"},
	}
	if len(fs) != len(want) {
		t.Fatalf("findings %+v", fs)
	}
	for i := range want {
		if fs[i] != want[i] {
			t.Errorf("finding %d = %+v, want %+v", i, fs[i], want[i])
		}
	}
	if len(Parse(NoFindings)) != 0 {
		t.Error("NO_FINDINGS gave findings")
	}
	var many []string
	for i := 0; i < 30; i++ {
		many = append(many, "- [P1] f.go:"+string(rune('1'+i%9))+strings.Repeat("0", i/9)+" — bug")
	}
	if n := len(Parse(strings.Join(many, "\n"))); n != MaxFindings {
		t.Errorf("not capped: %d", n)
	}
}

// AI text cannot ping people, hide the marker or grow without end.
func TestParseCleansText(t *testing.T) {
	fs := Parse("- [P0] a.go:1 — ask @alice <!-- termocode-review --> " + strings.Repeat("y", 500))
	if len(fs) != 1 {
		t.Fatalf("findings %+v", fs)
	}
	tx := fs[0].Text
	if strings.Contains(tx, "@alice") || strings.Contains(tx, "<!--") || len([]rune(tx)) > maxText {
		t.Errorf("text not cleaned: %q", tx)
	}
}

func TestComment(t *testing.T) {
	none := Comment(nil, "origin/main", false)
	if !strings.HasPrefix(none, Marker) || !strings.Contains(none, "No high-priority problems") {
		t.Errorf("empty comment:\n%s", none)
	}
	fs := []Finding{{Priority: "P0", File: "a.go", Line: 4, Text: "crash"}, {Priority: "P1", File: "b.go", Text: "race"}}
	c := Comment(fs, "origin/main", true)
	for _, want := range []string{Marker, "2 high-priority findings", "1 P0, 1 P1", "**P0** `a.go:4` — crash", "**P1** `b.go` — race", "first part"} {
		if !strings.Contains(c, want) {
			t.Errorf("comment misses %q:\n%s", want, c)
		}
	}
}

func TestFails(t *testing.T) {
	p0 := []Finding{{Priority: "P0"}}
	p1 := []Finding{{Priority: "P1"}}
	cases := []struct {
		fs   []Finding
		on   string
		want bool
	}{
		{p0, "p0", true}, {p1, "p0", false}, {p1, "p1", true}, {p0, "p1", true},
		{p0, "never", false}, {nil, "p1", false},
	}
	for _, c := range cases {
		if got := Fails(c.fs, c.on); got != c.want {
			t.Errorf("Fails(%v, %s) = %v", c.fs, c.on, got)
		}
	}
	if !ValidFailOn("p0") || ValidFailOn("p2") {
		t.Error("ValidFailOn")
	}
}

func TestEventPR(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if n, err := EventPR(write("pr.json", `{"pull_request":{"number":7}}`)); err != nil || n != 7 {
		t.Errorf("pull_request: %d %v", n, err)
	}
	if n, err := EventPR(write("c.json", `{"issue":{"number":9,"pull_request":{"url":"x"}}}`)); err != nil || n != 9 {
		t.Errorf("issue_comment: %d %v", n, err)
	}
	if _, err := EventPR(write("i.json", `{"issue":{"number":9}}`)); err == nil {
		t.Error("a plain issue gave a pull request")
	}
	if _, err := EventPR(write("bad.json", `{`)); err == nil {
		t.Error("bad JSON")
	}
	if _, err := EventPR(filepath.Join(dir, "none.json")); err == nil {
		t.Error("missing file")
	}
}

// The workflow runs the review, posts it, and lets only members start it
// from a comment.
func TestWorkflow(t *testing.T) {
	for _, want := range []string{"termocode\" review", "--post \"$PR\"", "pull_request:", "/termocode review",
		"author_association", "ANTHROPIC_API_KEY", "fetch-depth: 0", "pull-requests: write"} {
		if !strings.Contains(Workflow, want) {
			t.Errorf("workflow misses %q", want)
		}
	}
	if strings.Contains(Workflow, "${{ github.event.comment.body }}") {
		t.Error("the comment body goes into a shell")
	}
}
