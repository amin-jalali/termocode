package app

import (
	"strings"
	"testing"
)

// Spec (owner, phone chat 2026-11-09): "can you connect to the database
// and see how many tables I have?" — the owner configured one database
// (BaaytonaDB, .termocode/databases.json); the AI connected BaaytonaDB,
// its query failed, and it then connected baaytona-db-1 on its own and
// answered from that one. The AI uses the connection the owner meant (the
// one named, else the project's own one), and when it fails it says that
// connection's exact error and stops: it never tries other connections
// unasked.
func TestAIUsesTheOneDatabaseAsked(t *testing.T) {
	for _, want := range []string{"project's own", "never try other connections", "exact error"} {
		if !strings.Contains(aiActRules, want) {
			t.Errorf("act rules miss %q: %q", want, aiActRules)
		}
	}
}
