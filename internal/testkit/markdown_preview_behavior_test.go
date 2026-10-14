package testkit

import (
	"strings"
	"testing"
	"time"
)

// F7 on a Markdown file opens the reader view: file name and "Preview"
// badge in the header, text in a centred column, clean heading rules,
// task boxes; a resize re-wraps it; esc closes it.
func TestMarkdownPreviewReaderView(t *testing.T) {
	files := textFiles()
	files["notes/read.md"] = "# Title\n\n" + strings.Repeat("word ", 80) + "\n\n## Part\n\n- [x] done\n- [ ] next\n"
	d := editorDriver(t, files)
	d.Resize(200, 40)
	open(d, "notes/read.md")
	d.Key("f7")
	if !d.WaitForText("━━━━", 5*time.Second) {
		t.Fatalf("reader view did not open:\n%s", screen(d))
	}
	CheckFrame(t, d.Frame(), 200, 40)
	rows := strings.Split(screen(d), "\n")
	if h := rows[0]; !strings.Contains(h, "read.md") || !strings.Contains(h, "Preview") {
		t.Errorf("header = %q", h)
	}
	for i, r := range rows[1 : len(rows)-1] {
		if strings.Contains(r, "word") {
			if lead := len(r) - len(strings.TrimLeft(r, " ")); lead != 50 {
				t.Errorf("row %d: text starts at %d, want 50 (centred 100-cell column)", i+1, lead)
			}
			if n := len([]rune(strings.TrimSpace(r))); n > 100 {
				t.Errorf("row %d: %d cells, more than the column", i+1, n)
			}
		}
	}
	scr := screen(d)
	for _, want := range []string{"Title", "Part", "done", "next"} {
		if !strings.Contains(scr, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(scr, "▌") || strings.Contains(scr, "# Title") {
		t.Errorf("old heading bars / raw marks:\n%s", scr)
	}

	d.Resize(90, 30)
	CheckFrame(t, d.Frame(), 90, 30)
	for _, r := range strings.Split(screen(d), "\n")[1:] {
		if strings.Contains(r, "word") && !strings.HasPrefix(r, "    w") {
			t.Errorf("after resize, text must re-wrap at the 4-cell margin: %q", r)
			break
		}
	}
	d.Key("esc")
	if strings.Contains(strings.Split(screen(d), "\n")[0], "Preview") {
		t.Error("esc did not close the reader view")
	}
}
