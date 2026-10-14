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
	mm := next
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
	img := markdownImage(dir)
	if rows := img("pic.png", "logo", 60); len(rows) < 3 {
		t.Fatalf("image rows missing: %d", len(rows))
	} else {
		for i, r := range rows {
			if w := lipgloss.Width(r); w > 60 {
				t.Errorf("row %d is %d cells, max 60", i, w)
			}
		}
	}
	for _, dest := range []string{"https://x/y.png", "a.svg", "missing.png"} {
		if img(dest, "x", 60) != nil {
			t.Errorf("%s: want nil (placeholder)", dest)
		}
	}
	md := "# Title\n\n![logo](pic.png)\n\n![remote](https://x/y.png)\n"
	out := strings.Join(markdownDoc(md, dir).Lines(80), "\n")
	if !strings.Contains(stripANSI(out), "[image: remote]") {
		t.Error("remote image must show the placeholder")
	}
	if strings.Count(out, "\n") < 8 {
		t.Errorf("local image rows missing:\n%s", out)
	}
}
