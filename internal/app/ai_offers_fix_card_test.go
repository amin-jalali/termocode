package app

import (
	"strings"
	"testing"
)

// Spec (owner, phone chat 2026-11-09): BaaytonaDB failed with "nothing
// answers at 127.0.0.1:5432 … 5432 only inside Docker" and the AI ended
// with "the port must be published or the project's connection changed to
// the container route" — words, no fix. When a configured thing fails and
// termocode's tools can fix it, the AI calls the fixing tool in the same
// turn: its card is the owner's yes or no; it never stops at describing.
func TestAIOffersTheFixOnACard(t *testing.T) {
	for _, want := range []string{"in the same turn", "the card is the question", "never stop at describing"} {
		if !strings.Contains(aiOwnToolsRule, want) {
			t.Errorf("own-tools rule misses %q", want)
		}
	}
}
