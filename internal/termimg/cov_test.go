package termimg

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muesli/termenv"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, gradientImage(w, h)); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestCarrierBeforeEqualsCarrier(t *testing.T) {
	if CarrierBefore("S", 2, 3, 7) != Carrier("S", 2, 3, 7) {
		t.Fatal("the fallback carrier differs")
	}
}

func TestKittyDeleteAll(t *testing.T) {
	if got := KittyDeleteAll(); !strings.HasPrefix(got, "\x1b_G") || !strings.Contains(got, "a=d") || !strings.HasSuffix(got, "\x1b\\") {
		t.Fatalf("got %q", got)
	}
}

func TestDecodeBytesAndErrors(t *testing.T) {
	img, err := DecodeBytes(pngBytes(t, 4, 3))
	if err != nil || img.Bounds().Dx() != 4 || img.Bounds().Dy() != 3 {
		t.Fatalf("%v %v", img, err)
	}
	if _, err := DecodeBytes([]byte("not an image")); err == nil {
		t.Fatal("garbage decoded")
	}
	// Header fine, pixel data cut short: Decode fails on the second pass.
	data := pngBytes(t, 40, 40)
	if _, err := DecodeBytes(data[:len(data)/2]); err == nil {
		t.Fatal("a cut file decoded")
	}
}

func TestDecodeFileErrors(t *testing.T) {
	if _, err := Decode(filepath.Join(t.TempDir(), "missing.png"), 0); err == nil {
		t.Fatal("missing file decoded")
	}
	p := filepath.Join(t.TempDir(), "a.png")
	if err := os.WriteFile(p, pngBytes(t, 8, 8), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(p, 10); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("size limit: %v", err)
	}
	if _, err := Decode(p, 1<<20); err != nil {
		t.Fatal(err)
	}
}

// A tall or wide image is capped to the room given, aspect kept.
func TestFitCapsColumnsAndRows(t *testing.T) {
	cols, rows := Fit(4000, 100, 20, 50, 10, 20)
	if cols != 20 || rows < 1 || rows > 50 {
		t.Fatalf("wide: %d x %d", cols, rows)
	}
	cols, rows = Fit(100, 4000, 50, 10, 10, 20)
	if rows != 10 || cols < 1 || cols > 50 {
		t.Fatalf("tall: %d x %d", cols, rows)
	}
}

func TestEncodePNGDownscalesAndKeepsSmall(t *testing.T) {
	for _, c := range []struct{ w, h, maxW, maxH int }{
		{100, 100, 10, 0}, {100, 100, 0, 10}, {1000, 2, 10, 10}, {2, 1000, 10, 10},
	} {
		b, err := EncodePNG(gradientImage(c.w, c.h), c.maxW, c.maxH)
		if err != nil {
			t.Fatal(err)
		}
		got, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if (c.maxW > 0 && got.Bounds().Dx() > c.maxW) || (c.maxH > 0 && got.Bounds().Dy() > c.maxH) || got.Bounds().Dx() < 1 || got.Bounds().Dy() < 1 {
			t.Fatalf("%+v -> %v", c, got.Bounds())
		}
	}
}

func TestNewCacheDefaultSize(t *testing.T) {
	c := NewCache(0)
	if c.max != 16 {
		t.Fatalf("max = %d", c.max)
	}
}

func TestProtocolAndModeNames(t *testing.T) {
	if Protocol(99).String() != "unknown" {
		t.Fatal("unknown protocol name")
	}
	for in, want := range map[string]Mode{
		" Sixel ": ModeSixel, "BLOCKS": ModeBlocks, "off": ModeOff, "kitty": ModeKitty, "whatever": ModeAuto, "": ModeAuto,
	} {
		if ParseMode(in) != want {
			t.Errorf("ParseMode(%q) != %q", in, want)
		}
	}
}

func TestCellSizeKnownAndDefault(t *testing.T) {
	if w, h := (Caps{CellW: 9, CellH: 18}).CellSize(); w != 9 || h != 18 {
		t.Fatalf("%d x %d", w, h)
	}
	if w, h := (Caps{}).CellSize(); w != 10 || h != 20 {
		t.Fatalf("%d x %d", w, h)
	}
}

// Forcing kitty or sixel inside tmux without passthrough falls back to
// blocks; with passthrough it is honoured.
func TestResolveForcedModesInTmux(t *testing.T) {
	blocked := Caps{InTmux: true}
	open := Caps{InTmux: true, Passthrough: true}
	if Resolve(ModeKitty, blocked, true) != Blocks || Resolve(ModeSixel, blocked, true) != Blocks {
		t.Fatal("forced graphics inside tmux must degrade")
	}
	if Resolve(ModeKitty, open, true) != Kitty || Resolve(ModeSixel, open, true) != Sixel {
		t.Fatal("passthrough should allow forced graphics")
	}
	if Resolve(ModeKitty, Caps{}, true) != Kitty {
		t.Fatal("forced kitty outside tmux")
	}
}

// tmuxPassthrough asks tmux; with a fake tmux on PATH it follows its answer
// and with none it says no.
func TestTmuxPassthroughReadsTmux(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if tmuxPassthrough() {
		t.Fatal("no tmux, no passthrough")
	}
	for out, want := range map[string]bool{"on": true, "all": true, "off": false} {
		script := "#!/bin/sh\necho " + out + "\n"
		if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := tmuxPassthrough(); got != want {
			t.Fatalf("tmux says %q: got %v", out, got)
		}
	}
}

// resample, sixel and blocks cope with empty and odd input.
func TestResampleAndSixelEdgeCases(t *testing.T) {
	if got := resample(gradientImage(4, 4), 0, 5); len(got) != 0 {
		t.Fatalf("zero width gave %d pixels", len(got))
	}
	// Upscaling a 1x1 image: each target pixel maps to an empty source range.
	one := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	one.Set(0, 0, color.NRGBA{200, 10, 10, 255})
	if got := resample(one, 6, 6); len(got) != 36 || got[35].R < 150 {
		t.Fatalf("upscale: %d pixels", len(got))
	}
	if SixelSeq(nil, 10, 10) != "" || SixelSeq(one, 0, 4) != "" || SixelSeq(one, 4, -1) != "" {
		t.Fatal("empty sixel input must give no sequence")
	}
	if toRGBA(nil) != (color.RGBA{A: 255}) {
		t.Fatal("nil colour is opaque black")
	}
	c := color.RGBA{R: 1, G: 2, B: 3}
	if chanOf(c, 0) != 1 || chanOf(c, 1) != 2 || chanOf(c, 2) != 3 {
		t.Fatal("channels")
	}
	if got := medianCut(nil, 4); len(got) != 1 || got[0].A != 255 {
		t.Fatalf("empty histogram palette: %v", got)
	}
	// A flat image has one colour: the palette stays small.
	flat := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	if s := SixelSeq(flat, 8, 8); !strings.HasPrefix(s, "\x1bP") {
		t.Fatalf("flat sixel: %q", s)
	}
}

func TestRenderBlocksProfiles(t *testing.T) {
	img := gradientImage(6, 6)
	for _, p := range []termenv.Profile{termenv.TrueColor, termenv.ANSI256, termenv.ANSI, termenv.Ascii} {
		lines := RenderBlocks(img, 6, 3, color.Black, p)
		if len(lines) != 3 {
			t.Fatalf("profile %v: %d lines", p, len(lines))
		}
	}
	if RenderBlocks(nil, 3, 3, color.Black, termenv.TrueColor) != nil || RenderBlocks(img, 0, 3, color.Black, termenv.TrueColor) != nil {
		t.Fatal("empty input must give no lines")
	}
}
