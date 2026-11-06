package clistep

import (
	"testing"
	"time"
)

// Spec (AGX-11): the agents' limit lines on a terminal screen use the
// same reset words as the CLI steps: LimitReset reads them, IsLimit
// knows them.
func TestLimitResetScreenLines(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no tz data")
	}
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, berlin)
	for _, c := range []struct {
		line string
		want time.Time
	}{
		{"⎿  Claude usage limit reached. Your limit will reset at 2pm (Europe/Berlin).", time.Date(2026, 11, 6, 14, 0, 0, 0, berlin)},
		{"⎿  5-hour limit reached ∙ resets 4pm", time.Date(2026, 11, 6, 16, 0, 0, 0, berlin)},
		{"■ You've hit your usage limit. Upgrade to Pro (https://openai.com/chatgpt/pricing) or try again in 2 hours 5 minutes.", now.Add(2*time.Hour + 5*time.Minute)},
		{"limit reached, no time", time.Time{}},
	} {
		if got := LimitReset(c.line, now); !got.Equal(c.want) {
			t.Errorf("LimitReset(%q) = %v, want %v", c.line, got, c.want)
		}
		if !IsLimit(c.line) {
			t.Errorf("IsLimit(%q) = false", c.line)
		}
	}
}
