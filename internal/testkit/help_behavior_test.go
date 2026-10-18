package testkit

import (
	"strings"
	"testing"
	"time"
)

// header is the first row of the screen (the reader's title row).
func header(d *Driver) string { return strings.Split(screen(d), "\n")[0] }

// waitHeader waits until the reader's title row shows text.
func waitHeader(d *Driver, text string) bool {
	d.T.Helper()
	return d.WaitFor(func() bool { return strings.Contains(header(d), text) }, 3*time.Second)
}

// "Help: Open Guide…" opens a built-in guide page in the reader with
// the Guide badge; Tab + Enter and a click follow links inside the
// viewer, Backspace comes back, Esc closes to the screen as before.
func TestHelpGuideViewer(t *testing.T) {
	d := New(t, Options{W: 120, H: 34})
	before := screen(d)

	d.Palette("help-guide")
	d.Type("Use AI")
	if !d.WaitForText("Use AI", 3*time.Second) {
		t.Fatalf("guide picker has no Use AI:\n%s", screen(d))
	}
	d.Key("enter")
	if !waitHeader(d, "Guide") || !strings.Contains(header(d), "Use AI") {
		t.Fatalf("guide did not open:\n%s", screen(d))
	}
	CheckFrame(t, d.Frame(), 120, 34)
	rows := strings.Split(screen(d), "\n")
	if f := rows[len(rows)-1]; !strings.Contains(f, "tab links") || strings.Contains(f, "backspace back") {
		t.Errorf("footer = %q", f)
	}

	// Tab picks the first link in view ("Set up AI"), Enter follows it.
	d.Key("tab")
	d.Key("enter")
	if !waitHeader(d, "Set up AI") {
		t.Fatalf("Tab + Enter did not open Set up AI:\n%s", screen(d))
	}
	CheckFrame(t, d.Frame(), 120, 34)
	rows = strings.Split(screen(d), "\n")
	if f := rows[len(rows)-1]; !strings.Contains(f, "backspace back") {
		t.Errorf("footer has no back key: %q", f)
	}

	d.Key("backspace")
	if !waitHeader(d, "Use AI") {
		t.Fatalf("Backspace did not come back:\n%s", screen(d))
	}

	// A click on the link follows it too.
	x, y, ok := find(d, "Set up AI")
	if !ok || y == 0 {
		t.Fatalf("no Set up AI link on screen:\n%s", screen(d))
	}
	d.Click(x+1, y)
	if !waitHeader(d, "Set up AI") {
		t.Fatalf("a click on the link did not open Set up AI:\n%s", screen(d))
	}

	d.Key("esc")
	if strings.Contains(header(d), "Guide") {
		t.Fatalf("esc did not close the guide:\n%s", screen(d))
	}
	CheckFrame(t, d.Frame(), 120, 34)
	if after := screen(d); after != before {
		t.Errorf("screen after esc differs from before:\n--- before\n%s\n--- after\n%s", before, after)
	}
}
