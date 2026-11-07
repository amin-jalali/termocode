package mdview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Spec (CMP-70): a mermaid or csv block in the preview is drawn as a
// diagram on the code card, labelled "<lang> · diagram"; a block that does
// not parse keeps its source. Every row stays exactly the width.
func TestDiagramBlocks(t *testing.T) {
	withMode(t, theme.IconModeUnicode)
	src := "# D\n\n```mermaid\ngraph TD\nA[Start] --> B[End]\n```\n\n```csv\nk,v\nx,1\n```\n\n```mermaid\ngantt\n title later\n```\n"
	rows := Render(src, Options{Width: 80})
	for i, r := range rows {
		if w := ansi.StringWidth(r); w != 80 {
			t.Fatalf("row %d is %d cells", i, w)
		}
	}
	s := strings.Join(plain(rows), "\n")
	for _, w := range []string{"mermaid · diagram", "csv · diagram", "Start", "▼", "┌───┬───┐", "title later"} {
		if !strings.Contains(s, w) {
			t.Fatalf("missing %q in\n%s", w, s)
		}
	}
	if strings.Contains(s, "A[Start]") {
		t.Fatalf("the drawn flowchart still shows its source:\n%s", s)
	}
}
