package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReviewRulesMarkdownEdges(t *testing.T) {
	if _, _, ok := mdHeading("    # four spaces is code"); ok {
		t.Error("four spaces make a code block, not a heading")
	}
	if _, _, ok := mdHeading("#nospace"); ok {
		t.Error("a heading needs a space")
	}
	if l, title, ok := mdHeading("## Code review ##"); !ok || l != 2 || title != "Code review" {
		t.Errorf("heading %d %q %v", l, title, ok)
	}
	if fenceStep("", "~~~go") != "~~~" || fenceStep("~~~", "code") != "~~~" || fenceStep("~~~", "~~~") != "" {
		t.Error("tilde fences")
	}
	text := "# Rules\n\nkeep this\n\n\n## Review rules\n\nno globals\n\n\n\n# Other\n\ntext\n"
	rest, review := SplitReviewRules(text)
	if review != "no globals" {
		t.Errorf("review %q", review)
	}
	if strings.Contains(rest, "\n\n\n") || !strings.Contains(rest, "keep this") || !strings.Contains(rest, "# Other") {
		t.Errorf("rest %q", rest)
	}
	// scoped rules: only for paths under their folder; without a folder or text they are ignored
	r := Rules{Review: "global", Scoped: []ScopedRule{
		{Dir: "api", File: "api/AGENTS.md", Review: "api only"},
		{Dir: "", Review: "no dir"},
		{Dir: "web", Review: ""},
	}}
	if got := r.ReviewFor([]string{"./api/x.go"}); !strings.Contains(got, "api only") || strings.Contains(got, "no dir") {
		t.Errorf("review for api: %q", got)
	}
	if got := r.ReviewFor([]string{"web/y.go"}); got != "global" {
		t.Errorf("review for web: %q", got)
	}
	big := Rules{Review: strings.Repeat("r", RulesMaxBytes+100)}
	if got := big.ReviewFor(nil); len(got) > RulesMaxBytes+200 {
		t.Errorf("review rules are capped, got %d", len(got))
	}
}

func TestRouteRuleWordCheck(t *testing.T) {
	r := Router{Rules: []RouteRule{{Slot: SlotMain, Words: make([]string, MaxRouteWords+1)}}}
	for i := range r.Rules[0].Words {
		r.Rules[0].Words[i] = "w"
	}
	if err := CheckRouter(r); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Errorf("too many words: %v", err)
	}
	if hasWord("anything", "") {
		t.Error("an empty word is never found")
	}
}

func TestProfileEditJSONEdges(t *testing.T) {
	if _, err := rawProfiles("p.json", map[string]json.RawMessage{"profiles": json.RawMessage(`[1]`)}); err == nil {
		t.Error("profiles that is not an object")
	}
	if p, err := rawProfiles("p.json", map[string]json.RawMessage{"profiles": json.RawMessage(`null`)}); err != nil || len(p) != 0 {
		t.Errorf("null profiles: %v %v", p, err)
	}
	if _, err := mergeProfileJSON(json.RawMessage(`{"a":1}`), json.RawMessage(`[`)); err == nil {
		t.Error("a broken new profile")
	}
	if got, err := mergeProfileJSON(json.RawMessage(`null`), json.RawMessage(`{"x":1}`)); err != nil || string(got) != `{"x":1}` {
		t.Errorf("a null old value is replaced: %s %v", got, err)
	}
}
