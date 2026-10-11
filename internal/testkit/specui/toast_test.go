package specui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/testkit"
)

// Spec: Given a 100x30 window with a long file open and the cursor near
// the bottom of the editor,
// when two notifications arrive (one with a detail line),
// then only the newest shows, as a narrow one-line card (title · detail)
// with no shadow, and it moves away from the bottom-right corner so the
// cursor line stays fully visible.
func TestToastFitsSmallWindow(t *testing.T) {
	slow(t)
	var b strings.Builder
	for i := 1; i <= 80; i++ {
		fmt.Fprintf(&b, "row %02d %s\n", i, strings.Repeat("text ", 10))
	}
	d := testkit.New(t, testkit.Options{W: 100, H: 30, Nvim: true,
		Files: map[string]string{"long.txt": b.String()}})
	d.Action("open:long.txt")
	mustSee(t, d, "row 01")
	d.Key("ctrl+end", "up") // row 79: the next-to-last row on screen
	mustSee(t, d, "row 80")
	const ln = 79
	d.Action("toast:Older|first detail")
	d.Action("toast:Saved|internal/statusbar/model.go · 480 lines")
	mustSee(t, d, "Saved")

	rows := strings.Split(screen(d), "\n")
	if strings.Contains(screen(d), "Older") {
		t.Errorf("only the newest toast should show at this size:\n%s", screen(d))
	}
	_, y, _ := locate(d, "Saved")
	if !strings.Contains(rows[y], "Saved · internal") {
		t.Errorf("title and detail should share one row: %q", rows[y])
	}
	if y < 1 || y+1 >= len(rows) || !strings.Contains(rows[y-1], "╭") || !strings.Contains(rows[y+1], "╰") {
		t.Errorf("card should be 3 rows tall around %q:\n%s", rows[y], screen(d))
	} else {
		top := rows[y-1]
		x0 := ansi.StringWidth(top[:strings.Index(top, "╭")])
		x1 := ansi.StringWidth(top[:strings.Index(top, "╮")])
		if w := x1 - x0 + 1; w > 40 {
			t.Errorf("card is %d cells wide, want at most 40:\n%s", w, screen(d))
		}
	}
	want := regexp.MustCompile(fmt.Sprintf(`row %02d( text){6}`, ln))
	if !want.MatchString(screen(d)) {
		t.Errorf("cursor line %d is covered by the toast:\n%s", ln, screen(d))
	}
}
