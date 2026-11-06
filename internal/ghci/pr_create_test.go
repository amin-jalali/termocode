package ghci

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompose(t *testing.T) {
	title, body := Compose([]string{"feat: parser", " fix: edge ", "", "docs: notes"})
	if title != "feat: parser" || body != "- feat: parser\n- fix: edge\n- docs: notes" {
		t.Errorf("title %q body %q", title, body)
	}
	if title, body = Compose([]string{"only one"}); title != "only one" || body != "" {
		t.Errorf("single: %q %q", title, body)
	}
	if title, body = Compose(nil); title != "" || body != "" {
		t.Errorf("none: %q %q", title, body)
	}
}

func TestDefaultBranch(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"repo view": {out: `{"defaultBranchRef":{"name":"trunk"}}`}}}
	if b, err := DefaultBranch(context.Background(), f.run, "/r"); err != nil || b != "trunk" {
		t.Errorf("%q %v", b, err)
	}
	f = &fakeGH{answers: map[string]fakeAnswer{"repo view": {out: `{}`}}}
	if _, err := DefaultBranch(context.Background(), f.run, "/r"); err == nil {
		t.Error("empty default accepted")
	}
}

// Create passes the text as separate arguments (no shell) and returns the
// URL gh prints last.
func TestCreate(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"pr create": {out: "Creating pull request for feat/x into main\n\nhttps://github.com/o/r/pull/15\n"}}}
	url, err := Create(context.Background(), f.run, "/r", NewPR{Title: "Add; rm -rf", Body: "- a\n- b", Base: "main", Branch: "feat/x", Draft: true})
	if err != nil || url != "https://github.com/o/r/pull/15" {
		t.Fatalf("%q %v", url, err)
	}
	want := []string{"pr", "create", "--title", "Add; rm -rf", "--body", "- a\n- b", "--head", "feat/x", "--base", "main", "--draft"}
	if strings.Join(f.calls[0], "\x00") != strings.Join(want, "\x00") {
		t.Errorf("args %q", f.calls[0])
	}
}

func TestCreateRefuses(t *testing.T) {
	f := &fakeGH{}
	for _, p := range []NewPR{{Title: " ", Branch: "x"}, {Title: "t"}, {Title: "t", Branch: "-x"}} {
		if _, err := Create(context.Background(), f.run, "/r", p); err == nil {
			t.Errorf("%+v accepted", p)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("gh called %d times", len(f.calls))
	}
}

// Reviewers and labels go as one comma list each; a name that looks like
// a flag or holds a space is refused before gh runs.
func TestCreateReviewersLabels(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"pr create": {out: "https://github.com/o/r/pull/16\n"}}}
	_, err := Create(context.Background(), f.run, "/r", NewPR{Title: "t", Branch: "feat/x", Base: "main",
		Reviewers: []string{"ann", "org/team"}, Labels: []string{"bug", "needs review"}})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(f.calls[0], "\x00")
	if !strings.Contains(got, "--reviewer\x00ann,org/team") || !strings.Contains(got, "--label\x00bug,needs review") {
		t.Errorf("args %q", f.calls[0])
	}
	for _, p := range []NewPR{
		{Title: "t", Branch: "x", Reviewers: []string{"-x"}},
		{Title: "t", Branch: "x", Reviewers: []string{"a b"}},
		{Title: "t", Branch: "x", Labels: []string{"a,b"}},
	} {
		g := &fakeGH{}
		if _, err := Create(context.Background(), g.run, "/r", p); err == nil || len(g.calls) != 0 {
			t.Errorf("%+v accepted", p)
		}
	}
}

// ParseNames splits a typed list on commas (and spaces for logins) and
// drops a leading @.
func TestParseNames(t *testing.T) {
	if got := strings.Join(ParseNames(" @ann, bob  cy,,", true), "|"); got != "ann|bob|cy" {
		t.Errorf("logins %q", got)
	}
	if got := strings.Join(ParseNames("bug, needs review ,", false), "|"); got != "bug|needs review" {
		t.Errorf("labels %q", got)
	}
}

// PRTemplate finds the repository's pull request template in the places
// GitHub looks, in any case.
func TestPRTemplate(t *testing.T) {
	dir := t.TempDir()
	if _, _, ok := PRTemplate(dir); ok {
		t.Error("a template in an empty folder")
	}
	if err := os.MkdirAll(filepath.Join(dir, ".github"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".github", "PULL_REQUEST_TEMPLATE.md"), []byte("## Summary\n\n## Tests\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	text, name, ok := PRTemplate(dir)
	if !ok || text != "## Summary\n\n## Tests\n" || name != ".github/PULL_REQUEST_TEMPLATE.md" {
		t.Errorf("%q %q %v", text, name, ok)
	}
}

// WithTemplate puts the template first and the commit list under it.
func TestWithTemplate(t *testing.T) {
	if got := WithTemplate("## Summary\n\n", "- a\n- b"); got != "## Summary\n\n- a\n- b" {
		t.Errorf("%q", got)
	}
	if got := WithTemplate("", "- a"); got != "- a" {
		t.Errorf("%q", got)
	}
	if got := WithTemplate("## S", ""); got != "## S" {
		t.Errorf("%q", got)
	}
}
