package app

import (
	"strings"
	"testing"
)

// Spec (owner, 2026-11-09): BaaytonaDB (the project's connection) failed
// with nothing at 127.0.0.1:5432, while baaytona-db-1 reaches the same
// database through its container and works; the AI reported the error and
// ignored the working one. "If it does not know, it asks; and it looks at
// the project's context first." When it is unclear which connection is
// meant, or the meant one fails, the AI looks at the project and at
// db_connections, then asks the owner one short question with the choices;
// it connects to another connection only after the owner says yes.
func TestAIAsksWhichDatabaseWhenUnsure(t *testing.T) {
	for _, want := range []string{
		"look at the project first",
		"same database",
		"ask the owner in one short question",
		"only after the owner says yes",
	} {
		if !strings.Contains(aiActRules, want) {
			t.Errorf("act rules miss %q", want)
		}
	}
	if strings.Contains(aiActRules, "never explore other connections") {
		t.Error("act rules still forbid looking at other connections at all; they must be looked at and offered")
	}
}
