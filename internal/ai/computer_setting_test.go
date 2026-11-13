package ai

import (
	"encoding/json"
	"testing"
)

// CMP-90: ai_computer_use is off unless set to true.
func TestComputerUseSetting(t *testing.T) {
	for _, c := range []struct {
		json string
		want bool
	}{
		{`{}`, false},
		{`{"ai_computer_use": false}`, false},
		{`{"ai_computer_use": true}`, true},
	} {
		var s Settings
		if err := json.Unmarshal([]byte(c.json), &s); err != nil {
			t.Fatal(err)
		}
		if got := s.ComputerUseOn(); got != c.want {
			t.Errorf("%s: %v, want %v", c.json, got, c.want)
		}
	}
}

// CMP-90: a desktop action is high risk, so autopilot and allow rules
// never run it without the card.
func TestComputerToolsAreHighRisk(t *testing.T) {
	for _, n := range []string{"computer_click", "computer_type", "computer_key", "computer_screenshot"} {
		if r := ClassifyRisk(Tool{Name: n, Mutating: true}, map[string]any{}); r != RiskHigh {
			t.Errorf("%s: %s", n, r)
		}
	}
}
