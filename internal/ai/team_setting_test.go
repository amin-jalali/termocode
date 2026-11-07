package ai

import (
	"encoding/json"
	"testing"
)

// OMX-40: ai_team_mode is off unless set to true.
func TestTeamModeSetting(t *testing.T) {
	for _, c := range []struct {
		json string
		want bool
	}{
		{`{}`, false},
		{`{"ai_team_mode": false}`, false},
		{`{"ai_team_mode": true}`, true},
	} {
		var s Settings
		if err := json.Unmarshal([]byte(c.json), &s); err != nil {
			t.Fatal(err)
		}
		if got := s.TeamMode(); got != c.want {
			t.Errorf("%s: %v, want %v", c.json, got, c.want)
		}
	}
}
