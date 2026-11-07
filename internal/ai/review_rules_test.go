package ai

import (
	"strings"
	"testing"
)

// CMP-37: a "## Code Review Rules" section of AGENTS.md is for reviews
// only: it is cut out of the chat rules and kept apart.
func TestSplitReviewRules(t *testing.T) {
	text := "Use tabs.\n\n## Code Review Rules\n\nFlag missing tests.\n### Detail\nNo panics.\n\n## Style\nShort names."
	rest, review := SplitReviewRules(text)
	if rest != "Use tabs.\n\n## Style\nShort names." {
		t.Errorf("rest %q", rest)
	}
	if review != "Flag missing tests.\n### Detail\nNo panics." {
		t.Errorf("review %q", review)
	}

	// Other heading names, any level, a colon, and two sections.
	rest, review = SplitReviewRules("# Review guidelines:\nA\n# Build\nB\n## review rules\nC")
	if rest != "# Build\nB" || review != "A\n\nC" {
		t.Errorf("rest %q review %q", rest, review)
	}

	// A heading inside a code fence is text.
	in := "Top\n```\n## Code Review Rules\n```\nEnd"
	if rest, review = SplitReviewRules(in); rest != in || review != "" {
		t.Errorf("fence: rest %q review %q", rest, review)
	}
	// A fence inside the section does not end it.
	_, review = SplitReviewRules("## Code Review\nx\n```\n# not a heading\n```\ny")
	if review != "x\n```\n# not a heading\n```\ny" {
		t.Errorf("fence in section %q", review)
	}
	// No section: the text stays as it is.
	if rest, review = SplitReviewRules("Just rules."); rest != "Just rules." || review != "" {
		t.Errorf("none: %q %q", rest, review)
	}
	// "Reviewing" or "Review process" is not the section.
	if _, review = SplitReviewRules("## Review process\nx"); review != "" {
		t.Errorf("wrong heading taken %q", review)
	}
}

// CMP-37: LoadProjectRules keeps the review rules out of Text, in Review,
// with folder ones per folder; the hash covers them.
func TestLoadProjectRulesReview(t *testing.T) {
	root := t.TempDir()
	writeRule(t, root, "AGENTS.md", "Use tabs.\n\n## Code Review Rules\nOnly P0 and P1.\n")
	writeRule(t, root, "api/AGENTS.md", "## Code Review Rules\nCheck auth on every handler.\n")
	r := LoadProjectRules(root)
	if r.Text != "Use tabs." {
		t.Errorf("text %q", r.Text)
	}
	if r.Review != "Only P0 and P1." {
		t.Errorf("review %q", r.Review)
	}
	if r.Hash == "" {
		t.Error("no hash")
	}
	if got := r.ReviewFor([]string{"api/user.go"}); !strings.Contains(got, "Only P0 and P1.") || !strings.Contains(got, "# api/AGENTS.md\nCheck auth") {
		t.Errorf("review for api %q", got)
	}
	if got := r.ReviewFor([]string{"web/x.go"}); got != "Only P0 and P1." {
		t.Errorf("review for web %q", got)
	}
	// The folder rule has no text left for read_file.
	var j RulesInjector
	if got := j.Inject(r, "api/user.go"); got != "" {
		t.Errorf("empty folder rule injected %q", got)
	}
	if !strings.Contains(r.Preview(), "Only P0 and P1.") {
		t.Errorf("the trust card does not show the review rules: %q", r.Preview())
	}

	// A file with only review rules is listed and has a hash.
	root2 := t.TempDir()
	writeRule(t, root2, "AGENTS.md", "## Review Rules\nBe brief.\n")
	r2 := LoadProjectRules(root2)
	if r2.Text != "" || r2.Review != "Be brief." || r2.Hash == "" || len(r2.Files) != 1 {
		t.Errorf("review only %+v", r2)
	}
	if line := ReviewRulesLine("Use tabs.\n\n## Code Review Rules\nx"); line != 3 {
		t.Errorf("heading line %d", line)
	}
	if line := ReviewRulesLine("none"); line != 0 {
		t.Errorf("no heading line %d", line)
	}
}
