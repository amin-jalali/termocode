package testkit

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// statusLnCol is the "Ln N, Col M" text of the status bar ("" if none).
func statusLnCol(d *Driver) string {
	lines := strings.Split(screen(d), "\n")
	last := lines[len(lines)-1]
	i := strings.Index(last, "Ln ")
	if i < 0 {
		return ""
	}
	s := last[i:]
	if j := strings.Index(s, "  "); j >= 0 {
		s = s[:j]
	}
	return strings.TrimSpace(s)
}

// Spec: Given a 100-line file open (the view must scroll to show line 80),
// when the user goes to line 80 (Ctrl+G 80) and moves 3 to the right,
// then the status bar says "Ln 80, Col 4": the cursor's line and column
// in the file, not its row and column on the screen.
func TestStatusBarShowsFileLineNotScreenRow(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&b, "line%03d\n", i)
	}
	d := editorDriver(t, map[string]string{"long.txt": b.String()})
	open(d, "long.txt")
	d.Key("ctrl+g")
	d.Type("80")
	d.Key("enter")
	d.Key("right", "right", "right")
	if got := cursor(d); got != "80:4" {
		t.Fatalf("nvim cursor = %s, want 80:4", got)
	}
	var got string
	d.WaitFor(func() bool { got = statusLnCol(d); return got == "Ln 80, Col 4" }, 2*time.Second)
	if got != "Ln 80, Col 4" {
		t.Errorf("status bar = %q, want \"Ln 80, Col 4\"", got)
	}
}
