package bugspec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Spec (CMP-79): a new bugfix spec is .termocode/specs/<slug>/bugfix.md
// with the title and the three sections (current, expected, unchanged)
// plus steps; each section starts with a comment that says what goes in
// it. The template alone is not ready: every one of the three sections
// is reported empty.
func TestTemplateAndProblems(t *testing.T) {
	if got := PathFor("/w", "Toast: queue drops the 2nd message!"); got != filepath.Join("/w", ".termocode", "specs", "toast-queue-drops-the-2nd-message", "bugfix.md") {
		t.Errorf("path %q", got)
	}
	if got := Slug("  ***  "); got != "bug" {
		t.Errorf("empty slug %q", got)
	}
	if got := Slug(strings.Repeat("word ", 40)); len(got) > 48 || strings.HasSuffix(got, "-") {
		t.Errorf("long slug %q", got)
	}
	md := Template("Toast queue drops a message")
	for _, want := range []string{"# Bugfix: Toast queue drops a message", "## Current behavior", "## Expected behavior", "## Unchanged behavior", "## Steps to reproduce", "<!--"} {
		if !strings.Contains(md, want) {
			t.Errorf("no %q in:\n%s", want, md)
		}
	}
	s := Parse(md)
	if s.Title != "Toast queue drops a message" || len(s.Current)+len(s.Expected)+len(s.Unchanged)+len(s.Steps) != 0 {
		t.Errorf("template parsed %+v", s)
	}
	p := s.Problems()
	if len(p) != 3 || !strings.Contains(p[0], "Current") || !strings.Contains(p[1], "Expected") || !strings.Contains(p[2], "Unchanged") {
		t.Errorf("problems %q", p)
	}
	if line := FirstEmptyLine(md, s); line < 1 || !strings.Contains(strings.Split(md, "\n")[line-1], "Current") {
		t.Errorf("first empty line %d", line)
	}
}

// Spec (CMP-79): the sections are read by their heading (any case, extra
// words allowed); list marks and comments (also over several lines) are
// dropped; each other line is one item.
func TestParse(t *testing.T) {
	s := Parse("# Bugfix: Two toasts\n\n## current BEHAVIOR (now)\n- Push twice: only one shows\n<!-- a\nmulti-line comment -->\n\n## Expected\n1. Both toasts show, in order\n* the second waits 2 s\n" +
		"## Unchanged behavior\nA single toast still closes after 4 s\n## Steps to reproduce\n- run x\n## Notes\nfree text\n")
	if s.Title != "Two toasts" {
		t.Errorf("title %q", s.Title)
	}
	if strings.Join(s.Current, "|") != "Push twice: only one shows" || strings.Join(s.Expected, "|") != "Both toasts show, in order|the second waits 2 s" ||
		strings.Join(s.Unchanged, "|") != "A single toast still closes after 4 s" || strings.Join(s.Steps, "|") != "run x" {
		t.Errorf("parsed %+v", s)
	}
	if p := s.Problems(); len(p) != 0 {
		t.Errorf("problems %q", p)
	}
}

// Spec (CMP-79): the fix prompt asks for the work test first: a failing
// test per expected line, a guard test per unchanged line that passes
// before the fix, then the smallest fix with every test green; it lists
// the lines and names the spec file.
func TestFixPrompt(t *testing.T) {
	s := Spec{Title: "Two toasts", Current: []string{"one shows"}, Expected: []string{"both show"}, Unchanged: []string{"one toast closes after 4 s"}}
	p := FixPrompt(".termocode/specs/two-toasts/bugfix.md", s)
	for _, want := range []string{".termocode/specs/two-toasts/bugfix.md", "Two toasts", "fails", "guard", "passes", "smallest", "- both show", "- one toast closes after 4 s", "- one shows"} {
		if !strings.Contains(p, want) {
			t.Errorf("no %q in:\n%s", want, p)
		}
	}
	if strings.Index(p, "fails") > strings.Index(p, "smallest") {
		t.Error("the fix comes before the failing test")
	}
}

// Spec (CMP-79): List finds every bugfix.md under .termocode/specs,
// newest first, with its title; no folder is no specs and no error.
func TestList(t *testing.T) {
	root := t.TempDir()
	if l, err := List(root); err != nil || len(l) != 0 {
		t.Fatalf("empty: %v %v", l, err)
	}
	for i, title := range []string{"Old bug", "New bug"} {
		p := PathFor(root, title)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(Template(title)), 0o644); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(time.Duration(i-2) * time.Hour)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, Dir, "feature-x"), 0o755); err != nil { // no bugfix.md: not listed
		t.Fatal(err)
	}
	l, err := List(root)
	if err != nil || len(l) != 2 || l[0].Title != "New bug" || l[1].Title != "Old bug" || !strings.HasSuffix(l[0].Path, filepath.Join("new-bug", "bugfix.md")) {
		t.Errorf("list %+v %v", l, err)
	}
}
