package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/explorer"
	"github.com/amin-jalali/termocode/internal/termimg"
)

func writePNG(t *testing.T, dir string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 4), 128, 255})
		}
	}
	p := filepath.Join(dir, "pic.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImagePreviewOpensInsteadOfRefusing(t *testing.T) {
	withConfigDir(t)
	path := writePNG(t, t.TempDir(), 40, 30)
	m0, _ := New().Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := m0.(Model)
	next, cmd := m.updateInner(explorer.OpenFileMsg{Path: path})
	mm := next.(Model)
	if !mm.imageOpen || cmd == nil {
		t.Fatalf("image should open a preview (open=%v)", mm.imageOpen)
	}
	msg := cmd()
	nm, _ := mm.Update(msg)
	mm = nm.(Model)
	if mm.imagePrev == nil || mm.imagePrev.img == nil {
		t.Fatal("image not decoded")
	}
	termCaps = termimg.Caps{Protocol: termimg.Blocks}
	out := mm.View()
	lines := strings.Split(out, "\n")
	if len(lines) != 30 {
		t.Fatalf("frame has %d rows", len(lines))
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 100 {
			t.Fatalf("row %d is %d cells wide", i, w)
		}
	}
	if !strings.Contains(out, "pic.png") {
		t.Error("card title missing")
	}
	// Esc closes.
	nm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if nm.(Model).imageOpen {
		t.Error("esc should close the preview")
	}
}

func TestPlaceGraphicsAppendsCarrierToLastImageRow(t *testing.T) {
	frame := "aaaa\nbbbb\ncccc\ndddd"
	out := strings.Split(placeGraphics(frame, "\x1b_Ga=p\x1b\\", 1, 2, 2), "\n")
	if out[0] != "aaaa" || out[1] != "bbbb" || out[3] != "dddd" {
		t.Fatalf("other rows changed: %q", out)
	}
	if !strings.HasPrefix(out[2], "cccc\x1b7") || !strings.Contains(out[2], "\x1b[2;3H\x1b_Ga=p\x1b\\\x1b8") {
		t.Fatalf("carrier not appended to the last image row: %q", out[2])
	}
	if lipgloss.Width(out[2]) != 4 {
		t.Fatalf("carrier must be zero-width, row is %d", lipgloss.Width(out[2]))
	}
	// Out of range: frame untouched.
	if placeGraphics(frame, "SEQ", 3, 0, 5) != frame {
		t.Error("out-of-range placement must be a no-op")
	}
}

func TestMarkdownImagesInline(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, dir, 20, 10)
	md := "# Title\n\n![logo](pic.png)\n\n![remote](https://x/y.png) ![svg](a.svg)\n"
	content, paths := extractMarkdownImages(md, dir)
	if len(paths) != 1 || paths[0] != filepath.Join(dir, "pic.png") {
		t.Fatalf("paths = %v", paths)
	}
	if !strings.Contains(content, "https://x/y.png") || !strings.Contains(content, "a.svg") {
		t.Error("remote / svg references must stay untouched")
	}
	rendered := "line\n  " + mdImageToken(0) + "  \nend"
	out := injectMarkdownImages(rendered, paths, 60)
	if strings.Contains(out, "TCIMG") {
		t.Error("placeholder left in output")
	}
	if strings.Count(out, "\n") < 4 {
		t.Errorf("image rows missing:\n%s", out)
	}
}
