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

// Spec: the hover label of the chat's + button ends with its own line
// "More info"; the pointer can move onto that line (the label stays,
// the line lights up) and a click there opens "Use AI" at "Chat panel",
// that heading on the first content row. The click reaches nothing else.
func TestTooltipMoreInfoOpensGuide(t *testing.T) {
	d := New(t, Options{W: 120, H: 40, Git: true})
	if !tryAIPanel(d) {
		t.Fatal("no AI panel")
	}
	d.Action("toasts:clear")
	hoverTarget(t, d, "ai:new")
	text, x, y, topic, ok := d.Model().TooltipHelp()
	if !ok || topic != "guides/ai-use.md#chat-panel" || !strings.Contains(text, "More info") {
		t.Fatalf("ai:new help line: ok=%v topic=%q text=%q\n%s", ok, topic, text, screen(d))
	}
	if _, gy, found := find(d, "More info"); !found || gy != y {
		t.Fatalf("More info not drawn on row %d:\n%s", y, screen(d))
	}
	label, _, ly, _ := d.Model().Tooltip()
	if ly != y-1 || !strings.Contains(label, "New chat") {
		t.Errorf("help line is not the label's last line: label row %d %q, help row %d", ly, label, y)
	}
	d.Move(x+1, y)
	d.Settle(700 * time.Millisecond) // past the tick: the label stays
	if _, _, _, _, ok := d.Model().TooltipHelp(); !ok {
		t.Fatalf("label hid when the pointer moved onto More info:\n%s", screen(d))
	}
	if got := d.Model().ProbeClick(x+1, y); got != "tip:help" {
		t.Errorf("probeClick on More info = %q, want tip:help", got)
	}
	d.Click(x+1, y)
	if !waitHeader(d, "Use AI") || !strings.Contains(header(d), "Guide") {
		t.Fatalf("click on More info did not open the guide:\n%s", screen(d))
	}
	if first := strings.TrimSpace(strings.Split(screen(d), "\n")[1]); first != "Chat panel" {
		t.Errorf("first content row = %q, want Chat panel:\n%s", first, screen(d))
	}
	d.Key("esc")
	if strings.Contains(header(d), "Guide") {
		t.Fatalf("esc did not close the guide:\n%s", screen(d))
	}
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
