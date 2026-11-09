package app

import (
	"strings"
	"testing"
)

// Spec (owner, phone chat 2026-11-09): "connect and see which databases
// are defined" got the connection list and "not connected; the database
// tools connect when needed" — the model repeated the tool's own words and
// never connected. When the owner asks to connect or check, the AI
// connects (db_connection op connect, or reads with a db_* tool) and says
// what it found; the list's state never tells the model to leave it.
func TestAIConnectsWhenAsked(t *testing.T) {
	if !strings.Contains(aiActRules, "db_connection") || !strings.Contains(aiActRules, "connect") {
		t.Errorf("act rules do not say to connect when asked: %q", aiActRules)
	}
	if strings.Contains(aiDBNotConnectedState, "as needed") || strings.Contains(aiDBNotConnectedState, "when needed") {
		t.Errorf("db_connections state tells the model to wait: %q", aiDBNotConnectedState)
	}
}
