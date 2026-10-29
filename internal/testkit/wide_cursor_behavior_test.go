package testkit

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// Spec (SCN-11): Given the mixed line open with the cursor at its start,
// when the user presses Right five times,
// then the cursor is on the flag in nvim, the status bar column counts
// glyphs (Col 6), and the screen cell under the cursor shows the flag;
// and Shift+Right ×4 then Ctrl+C copies exactly "中文 👩‍💻".
func TestWideEditorCursorAndSelection(t *testing.T) {
	d := editorDriver(t, wideEditorFiles())
	open(d, "wide.txt")
	d.Key("right", "right", "right", "right", "right")
	under := lua(d, `local c = vim.fn.col('.'); local l = vim.fn.getline('.'); return vim.fn.strcharpart(l:sub(c), 0, 1)`)
	if under != "🇮" && under != wideFlag {
		t.Fatalf("cursor is on %q, want the flag (cursor %s)", under, cursor(d))
	}
	var got string
	d.WaitFor(func() bool { got = statusLnCol(d); return got == "Ln 1, Col 6" }, 2*time.Second)
	if got != "Ln 1, Col 6" {
		t.Errorf("status bar = %q, want \"Ln 1, Col 6\" (glyphs, not bytes)", got)
	}
	x, y, ok := find(d, "中文 ")
	if !ok {
		t.Fatalf("line not on screen:\n%s", screen(d))
	}
	vcol := lua(d, `return tostring(vim.fn.virtcol('.', 1)[1])`)
	vc, _ := strconv.Atoi(vcol)
	if c := cellsAt(d, x+vc-1, y, 2); c != wideFlag {
		t.Errorf("screen cell at the cursor (virtcol %s) shows %q, want the flag", vcol, c)
	}
	d.Key("home")
	d.Key("shift+right", "shift+right", "shift+right", "shift+right")
	d.Key("ctrl+c")
	d.Key("down", "down", "end")
	d.Key("ctrl+v")
	lines := strings.Split(bufLines(d), "\n")
	if want := wideFa + " and English 42" + "中文 " + wideDev; len(lines) < 3 || lines[2] != want {
		t.Errorf("pasted line = %q, want %q", lines[min(2, len(lines)-1)], want)
	}
	checkGlyphs(d, "after paste")
}
