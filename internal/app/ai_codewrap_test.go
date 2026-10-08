package app

import (
	"strings"
	"testing"
)

func TestCodeWrapKeepsIdentifiers(t *testing.T) {
	line := []mdSpan{{text: "    if !t.ExpiresAt.After(now) && !t.leaving {"}}
	rows := wrapStyledLine(line, 22)
	if len(rows) < 2 {
		t.Fatalf("expected a wrap, got %d rows", len(rows))
	}
	var joined []string
	for i, r := range rows {
		txt := joinParts(r)
		if w := len([]rune(txt)); w > 22 {
			t.Errorf("row %d too wide (%d): %q", i, w, txt)
		}
		if i > 0 && !strings.Contains(txt, "↪") && !strings.Contains(txt, "> ") {
			t.Errorf("continuation row %d lacks the ↪ marker: %q", i, txt)
		}
		joined = append(joined, strings.TrimSpace(strings.Replace(txt, "↪", "", 1)))
	}
	for _, ident := range []string{"ExpiresAt", "After", "leaving"} {
		found := false
		for _, j := range joined {
			if strings.Contains(j, ident) {
				found = true
			}
		}
		if !found {
			t.Errorf("identifier %q was split across rows: %q", ident, joined)
		}
	}
}

func TestAutoActionsWidth(t *testing.T) {
	if got := autoActionsWidth(200, true, 30); got != 80 {
		t.Errorf("wide window: %d, want 80", got)
	}
	if got := autoActionsWidth(150, true, 30); got != 56 {
		t.Errorf("150 cols: %d, want 56 (editor keeps 60)", got)
	}
	if got := autoActionsWidth(120, true, 30); got < defaultActionsWidth || 120-4-30-got < 60-1 && got > defaultActionsWidth {
		t.Errorf("120 cols: %d leaves the editor too narrow", got)
	}
}
