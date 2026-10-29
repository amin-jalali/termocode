package ai

import (
	"reflect"
	"testing"
	"time"
)

// Every kind the provider picker offers has its own name, label and
// default endpoint (only the custom-URL kind has none).
func TestEveryKindHasNames(t *testing.T) {
	seen := map[string]string{}
	for _, k := range Kinds {
		label := KindLabel(k)
		if label == "None" || label == "" {
			t.Errorf("%s: no label", k)
		}
		if other, dup := seen[label]; dup {
			t.Errorf("%s and %s share the label %q", k, other, label)
		}
		seen[label] = k
		if ProviderName(k) == k {
			t.Errorf("%s: no short name", k)
		}
		if (DefaultBaseURL(k) == "") != (k == KindCompatible) {
			t.Errorf("%s: base url %q", k, DefaultBaseURL(k))
		}
	}
	if KindLabel("mystery") != "None" || ProviderName("mystery") != "mystery" {
		t.Error("an unknown kind")
	}
}

func TestSuggestedModels(t *testing.T) {
	if got := SuggestedModels(KindClaudeOAuth); !reflect.DeepEqual(got, []string{ModelClaudeOpus, ModelClaudeSonnet, ModelClaudeHaiku}) {
		t.Errorf("claude = %v", got)
	}
	for _, k := range []string{KindAnthropic, KindOpenAI, KindOpenRouter, KindOllama} {
		if len(SuggestedModels(k)) == 0 {
			t.Errorf("%s: no suggestions", k)
		}
	}
	// A local LM Studio or a custom server has no list we can guess.
	if SuggestedModels(KindLMStudio) != nil || SuggestedModels(KindCompatible) != nil {
		t.Error("guessed models for an unknown server")
	}
}

func TestCLILabels(t *testing.T) {
	if CLILabel(CLIClaude) != "Claude Code (subscription)" || CLILabel(CLICodex) != "Codex (subscription)" {
		t.Errorf("labels %q %q", CLILabel(CLIClaude), CLILabel(CLICodex))
	}
	if CLILabel("aider") != "aider" {
		t.Error("an unknown CLI keeps its own name")
	}
}

func TestLimitErrorText(t *testing.T) {
	e := &LimitError{CLI: CLIClaude, Msg: "5-hour limit"}
	if e.Error() != "Claude Code usage limit reached" {
		t.Errorf("no reset: %q", e.Error())
	}
	reset := time.Date(2026, 1, 2, 15, 4, 0, 0, time.Local)
	e = &LimitError{CLI: CLICodex, ResetAt: reset}
	if e.Error() != "Codex usage limit reached (resets at 15:04)" {
		t.Errorf("with reset: %q", e.Error())
	}
}

func TestProfileSetAddKeepsOrder(t *testing.T) {
	var s ProfileSet
	s.Add(Profile{Name: "b", Title: "one"})
	s.Add(Profile{Name: "a"})
	s.Add(Profile{Name: "b", Title: "two"})
	if got := s.Names(); !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Errorf("names = %v", got)
	}
	if p, ok := s.Get("b"); !ok || p.Title != "two" {
		t.Errorf("b = %+v, %v", p, ok)
	}
}

func TestForkNote(t *testing.T) {
	if (Conversation{}).ForkNote() != "" {
		t.Error("a chat that is no fork has no note")
	}
	c := Conversation{ForkedFrom: &ForkInfo{ID: "x", Title: "Parser", Message: 3}}
	if got := c.ForkNote(); got != "Forked from Parser at message 3" {
		t.Errorf("note = %q", got)
	}
}

func TestInlineCacheEmptyAndOnlySpaceLeft(t *testing.T) {
	var c InlineCache
	if !c.Empty() {
		t.Error("a new cache is empty")
	}
	c.Remember(1, 0, "fmt.", "Println(x)  ")
	if c.Empty() {
		t.Error("a filled cache is not empty")
	}
	// Typed up to where only spaces are left: ask the model again.
	if _, ok := c.Match(1, 0, "fmt.Println(x)"); ok {
		t.Error("only spaces left must not be shown")
	}
	c.Reset()
	if !c.Empty() {
		t.Error("reset cache is empty")
	}
}
