//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// Spec (find-results-design §5 #13): in the real terminal path the Find
// Results page shows its summary and file header, a click on a match
// opens the file at that line, and F4 walks the matches.
func TestFindResultsPage(t *testing.T) {
	tm := boot(t, 100, 30)
	tm.Palette("Search: Find in Files")
	tm.WaitText("Find:", uiTimeout)
	tm.Type("note")
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("1 match across 1 file", uiTimeout)
	if s := tm.Screen(); !strings.Contains(s, "notes.txt") || !strings.Contains(s, "1   first note") {
		t.Fatalf("page lacks the header or the match row:\n%s", s)
	}
	tm.settle()
	tm.ClickText("1   first note")
	tm.WaitText("Ln 1, Col", uiTimeout)
	tm.WaitGone("1 match across 1 file", uiTimeout)

	// F4 from the file walks the results again (one match: same place).
	tm.Send("\x1bOS") // F4
	tm.settle()
	if s := tm.Screen(); !strings.Contains(s, "first note") {
		t.Errorf("F4 lost the match:\n%s", s)
	}
	tm.Quit()
}
