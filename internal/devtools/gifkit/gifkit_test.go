package gifkit

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The sidecar has one line per caption with its time range; the last one
// runs to the end of the clip.
func TestCaptionText(t *testing.T) {
	c := Clip{Name: "x", Note: "demo models", Frames: []Frame{{Hold: 3 * time.Second}, {Hold: 2500 * time.Millisecond}},
		Captions: []Caption{{At: 0, Text: "One."}, {At: 3 * time.Second, Text: "Two."}}}
	got := CaptionText(c)
	for _, want := range []string{"x.gif - 5.5 s", "demo models", " 0.0- 3.0 s  One.", " 3.0- 5.5 s  Two."} {
		if !strings.Contains(got, want) {
			t.Errorf("caption file lacks %q:\n%s", want, got)
		}
	}
}

// Every screen is exactly Rows lines, so frames of one clip have one size
// (a terminal emulator drops trailing blank rows).
func TestNormalizePadsToRows(t *testing.T) {
	for _, in := range []string{"a", "a\n\n\n", strings.Repeat("x\n", Rows+5)} {
		if got := strings.Count(Normalize(in), "\n") + 1; got != Rows {
			t.Errorf("Normalize(%.10q): %d lines, want %d", in, got, Rows)
		}
	}
}

// A clip of screenshots (the phone GIF) is encoded from its PNG files,
// at their size; Load finds them next to frames.json.
func TestEncodePNGFrames(t *testing.T) {
	dir := t.TempDir()
	for i, c := range []color.RGBA{{10, 20, 30, 255}, {200, 100, 50, 255}} {
		img := image.NewRGBA(image.Rect(0, 0, 39, 84))
		draw.Draw(img, img.Bounds(), &image.Uniform{c}, image.Point{}, draw.Src)
		f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%03d.png", i)))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	js := `{"name":"p","frames":[{"png":"000.png","hold_ns":500000000},{"png":"001.png","hold_ns":700000000}],"captions":[]}`
	if err := os.WriteFile(filepath.Join(dir, "frames.json"), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encode(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	g, err := gif.DecodeAll(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if g.Config.Width != 39 || g.Config.Height != 84 || len(g.Image) != 2 || g.Delay[0] != 50 || g.Delay[1] != 70 {
		t.Errorf("gif %dx%d, %d frames, delays %v", g.Config.Width, g.Config.Height, len(g.Image), g.Delay)
	}
}

// The title frame fills the screen and holds its text.
func TestTitleFrame(t *testing.T) {
	f := TitleFrame("termocode", "tagline")
	if n := strings.Count(f, "\n") + 1; n != Rows {
		t.Errorf("%d lines, want %d", n, Rows)
	}
	if !strings.Contains(f, "tagline") {
		t.Error("tagline missing")
	}
}
