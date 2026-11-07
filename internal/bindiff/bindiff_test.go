package bindiff

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func pngBytes(t *testing.T, w, h int, paint func(*image.RGBA)) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{10, 20, 30, 255})
		}
	}
	if paint != nil {
		paint(img)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func pdfBytes(text string) []byte {
	content := "BT /F1 12 Tf 72 712 Td (" + text + ") Tj ET"
	return []byte(fmt.Sprintf("%%PDF-1.4\n4 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\ntrailer\n<< >>\n%%%%EOF\n", len(content), content))
}

func TestSupported(t *testing.T) {
	for _, p := range []string{"a.png", "b/C.JPG", "x.jpeg", "y.gif", "z.webp", "doc.pdf", "D.PDF"} {
		if !Supported(p) {
			t.Errorf("Supported(%q) = false", p)
		}
	}
	for _, p := range []string{"a.go", "b.bin", "pdf", "x.svg"} {
		if Supported(p) {
			t.Errorf("Supported(%q) = true", p)
		}
	}
}

func TestImageSameSizeCountsChangedPixels(t *testing.T) {
	old := pngBytes(t, 10, 8, nil)
	nw := pngBytes(t, 10, 8, func(m *image.RGBA) {
		for y := 2; y <= 3; y++ {
			for x := 4; x <= 6; x++ {
				m.Set(x, y, color.RGBA{255, 0, 0, 255})
			}
		}
	})
	o, n, ok := Describe("logo.png", old, nw)
	if !ok {
		t.Fatal("not described")
	}
	for _, want := range []string{"Image · PNG", "Size: 10 × 8 px"} {
		if !strings.Contains(o, want) || !strings.Contains(n, want) {
			t.Errorf("both sides should say %q:\nold:\n%s\nnew:\n%s", want, o, n)
		}
	}
	if !strings.Contains(n, "Changed pixels: 6 of 80 (7.5%) in x 4–6, y 2–3") {
		t.Errorf("new side misses the pixel change:\n%s", n)
	}
	if strings.Contains(o, "Changed pixels") {
		t.Errorf("old side should not carry the change line:\n%s", o)
	}
}

func TestImageSamePixels(t *testing.T) {
	a := pngBytes(t, 4, 4, nil)
	_, n, ok := Describe("a.png", a, a)
	if !ok || !strings.Contains(n, "Pixels: the same") {
		t.Fatalf("want same pixels, got ok=%v\n%s", ok, n)
	}
}

func TestImageResized(t *testing.T) {
	o, n, ok := Describe("a.png", pngBytes(t, 4, 4, nil), pngBytes(t, 8, 2, nil))
	if !ok {
		t.Fatal("not described")
	}
	if !strings.Contains(o, "Size: 4 × 4 px") || !strings.Contains(n, "Size: 8 × 2 px") {
		t.Errorf("sizes wrong:\n%s\n---\n%s", o, n)
	}
	if !strings.Contains(n, "Pixels: not compared (the size changed)") {
		t.Errorf("want resize note:\n%s", n)
	}
}

func TestImageAddedAndDeleted(t *testing.T) {
	img := pngBytes(t, 3, 3, nil)
	o, n, ok := Describe("a.png", nil, img)
	if !ok || o != "" || !strings.Contains(n, "Size: 3 × 3 px") {
		t.Fatalf("added: ok=%v old=%q new=%q", ok, o, n)
	}
	o, n, ok = Describe("a.png", img, nil)
	if !ok || n != "" || !strings.Contains(o, "Size: 3 × 3 px") {
		t.Fatalf("deleted: ok=%v old=%q new=%q", ok, o, n)
	}
}

func TestImageBrokenSaysSo(t *testing.T) {
	_, n, ok := Describe("a.png", nil, []byte("not a png"))
	if !ok || !strings.Contains(n, "Could not read the image") {
		t.Fatalf("want a read note, got ok=%v %q", ok, n)
	}
}

func TestPDFDiffsTheText(t *testing.T) {
	o, n, ok := Describe("spec.pdf", pdfBytes("Hello old"), pdfBytes("Hello new"))
	if !ok {
		t.Fatal("not described")
	}
	if !strings.Contains(o, "PDF ·") || !strings.Contains(o, "Hello old") || !strings.Contains(n, "Hello new") {
		t.Errorf("pdf text missing:\n%s\n---\n%s", o, n)
	}
}

func TestPDFWithoutTextSaysSo(t *testing.T) {
	_, n, ok := Describe("scan.pdf", nil, []byte("%PDF-1.4\ntrailer\n%%EOF\n"))
	if !ok || !strings.Contains(n, "No text found") {
		t.Fatalf("want no-text note, got %q", n)
	}
}

func TestOtherFilesAreNotDescribed(t *testing.T) {
	if _, _, ok := Describe("a.bin", []byte{0, 1}, []byte{0, 2}); ok {
		t.Fatal("a.bin should not be described")
	}
}

func TestTooBigIsRefused(t *testing.T) {
	big := make([]byte, MaxBytes+1)
	_, n, ok := Describe("a.png", nil, big)
	if !ok || !strings.Contains(n, "Too big to compare") {
		t.Fatalf("want too-big note, got %q", n)
	}
}

func TestHumanSize(t *testing.T) {
	for in, want := range map[int]string{0: "0 B", 999: "999 B", 2048: "2.0 KB", 3 << 20: "3.0 MB"} {
		if got := humanSize(in); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", in, got, want)
		}
	}
}
