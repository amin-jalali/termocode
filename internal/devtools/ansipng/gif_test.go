package ansipng

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"testing"
	"time"
)

// EncodeGIF writes an animation whose frames, played back, are exactly
// the input frames (when they use ≤ 256 colours), with the given delays.
// Frames after the first only store the rectangle that changed.
func TestEncodeGIFRoundTrip(t *testing.T) {
	mk := func(fill color.RGBA, box image.Rectangle, boxC color.RGBA) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 40, 30))
		draw.Draw(img, img.Bounds(), &image.Uniform{fill}, image.Point{}, draw.Src)
		draw.Draw(img, box, &image.Uniform{boxC}, image.Point{}, draw.Src)
		return img
	}
	bg := color.RGBA{0x1e, 0x1e, 0x1e, 0xff}
	red := color.RGBA{0xe0, 0x40, 0x40, 0xff}
	blue := color.RGBA{0x40, 0x80, 0xe0, 0xff}
	frames := []*image.RGBA{
		mk(bg, image.Rect(2, 2, 10, 10), red),
		mk(bg, image.Rect(2, 2, 10, 10), red), // unchanged
		mk(bg, image.Rect(20, 15, 30, 25), blue),
	}
	frames[2] = func() *image.RGBA { // red box stays, blue box added
		img := mk(bg, image.Rect(2, 2, 10, 10), red)
		draw.Draw(img, image.Rect(20, 15, 30, 25), &image.Uniform{blue}, image.Point{}, draw.Src)
		return img
	}()
	delays := []time.Duration{time.Second, 200 * time.Millisecond, 1500 * time.Millisecond}
	var buf bytes.Buffer
	if err := EncodeGIF(&buf, frames, delays); err != nil {
		t.Fatal(err)
	}
	g, err := gif.DecodeAll(&buf)
	if err != nil {
		t.Fatal(err)
	}
	// The unchanged frame folds into the one before it (its time added).
	if len(g.Image) != 2 {
		t.Fatalf("frames: %d, want 2", len(g.Image))
	}
	if g.Delay[0] != 120 || g.Delay[1] != 150 {
		t.Errorf("delays %v, want [120 150] (1/100 s)", g.Delay)
	}
	if g.Image[1].Bounds() == g.Image[0].Bounds() {
		t.Errorf("second frame stores the whole picture %v, want only the change", g.Image[1].Bounds())
	}
	// Play back and compare.
	canvas := image.NewRGBA(frames[0].Bounds())
	want := []*image.RGBA{frames[0], frames[2]}
	for i, fr := range g.Image {
		draw.Draw(canvas, fr.Bounds(), fr, fr.Bounds().Min, draw.Over)
		for y := 0; y < 30; y++ {
			for x := 0; x < 40; x++ {
				if canvas.RGBAAt(x, y) != want[i].RGBAAt(x, y) {
					t.Fatalf("frame %d pixel %d,%d = %v, want %v", i, x, y, canvas.RGBAAt(x, y), want[i].RGBAAt(x, y))
				}
			}
		}
	}
}
