package ai

import (
	"strings"
	"testing"
)

// CMP-24: the effort dial bundles a profile and a reasoning level.

func TestDialDefaults(t *testing.T) {
	dial, problems := Settings{}.EffortDial()
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	want := map[string]DialSetting{
		DialLow:    {Profile: ProfileFast, Reasoning: ReasoningLow},
		DialMedium: {Profile: ProfileDefault, Reasoning: ReasoningMedium},
		DialHigh:   {Profile: ProfileDefault, Reasoning: ReasoningHigh},
		DialUltra:  {Profile: ProfileDefault, Reasoning: ReasoningMax},
	}
	for mode, w := range want {
		if dial[mode] != w {
			t.Errorf("%s = %+v, want %+v", mode, dial[mode], w)
		}
	}
	if len(dial) != len(DialModes) {
		t.Errorf("%d modes, want %d", len(dial), len(DialModes))
	}
}

func TestDialOverridesAndProblems(t *testing.T) {
	s := Settings{Dial: map[string]DialSetting{
		"ULTRA": {Profile: "until-done"},
		"high":  {Reasoning: "xhigh"},
		"turbo": {Profile: "fast"},
		"low":   {Reasoning: "lots"},
	}}
	dial, problems := s.EffortDial()
	if got := dial[DialUltra]; got.Profile != ProfileUntilDone || got.Reasoning != ReasoningMax {
		t.Errorf("ultra = %+v: a set field replaces only itself", got)
	}
	if got := dial[DialHigh]; got.Profile != ProfileDefault || got.Reasoning != ReasoningXHigh {
		t.Errorf("high = %+v", got)
	}
	if got := dial[DialLow]; got.Reasoning != ReasoningLow {
		t.Errorf("a bad level keeps the built-in one: %+v", got)
	}
	if _, ok := dial["turbo"]; ok {
		t.Error("an unknown mode is kept")
	}
	joined := strings.Join(problems, "\n")
	if len(problems) != 2 || !strings.Contains(joined, "turbo") || !strings.Contains(joined, "lots") {
		t.Errorf("problems: %v", problems)
	}
}

func TestNextDialMode(t *testing.T) {
	for cur, want := range map[string]string{"": DialLow, "low": DialMedium, "Medium": DialHigh, "high": DialUltra, "ultra": DialLow, "x": DialLow} {
		if got := NextDialMode(cur); got != want {
			t.Errorf("NextDialMode(%q) = %q, want %q", cur, got, want)
		}
	}
}

func TestDialModeOf(t *testing.T) {
	dial, _ := Settings{}.EffortDial()
	if got := DialModeOf(dial, ProfileDefault, ReasoningHigh); got != DialHigh {
		t.Errorf("got %q, want high", got)
	}
	if got := DialModeOf(dial, ProfileFast, ReasoningLow); got != DialLow {
		t.Errorf("got %q, want low", got)
	}
	if got := DialModeOf(dial, ProfileReview, ReasoningHigh); got != "" {
		t.Errorf("no mode matches review: %q", got)
	}
	if got := DialSummary(dial[DialUltra]); got != "profile default · think max" {
		t.Errorf("summary %q", got)
	}
	if got := DialSummary(DialSetting{}); got != "profile unchanged · the model's own thinking" {
		t.Errorf("empty summary %q", got)
	}
}
