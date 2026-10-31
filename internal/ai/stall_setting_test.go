package ai

import (
	"encoding/json"
	"testing"
	"time"
)

// OMX-23: ai_step_stall_minutes is the stall limit of an agent step:
// 10 minutes when not set, off with 0, capped at 120.
func TestStepStallSetting(t *testing.T) {
	for _, c := range []struct {
		json string
		want time.Duration
	}{
		{`{}`, DefaultStepStall},
		{`{"ai_step_stall_minutes": 0}`, 0},
		{`{"ai_step_stall_minutes": 3}`, 3 * time.Minute},
		{`{"ai_step_stall_minutes": 999}`, 120 * time.Minute},
		{`{"ai_step_stall_minutes": -2}`, DefaultStepStall},
	} {
		var s Settings
		if err := json.Unmarshal([]byte(c.json), &s); err != nil {
			t.Fatal(err)
		}
		if got := s.StepStall(); got != c.want {
			t.Errorf("%s: %v, want %v", c.json, got, c.want)
		}
	}
}
