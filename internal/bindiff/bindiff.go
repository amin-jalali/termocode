// Package bindiff turns the two sides of a binary file that people can
// still read (an image or a PDF) into short text, so the normal text diff
// can show what changed (CMP-54).
//
//   - Images (PNG, JPEG, GIF first frame, WebP): format, size in pixels and
//     file size on each side. When both sides have the same size, the new
//     side also says how many pixels changed and the box that holds them.
//   - PDFs: the file size and the text of the pages (best effort, see
//     internal/pdftext), so a text change in a PDF shows as lines.
//
// The package is pure: it reads only the bytes it is given.
package bindiff

import (
	"bytes"
	"fmt"
	"image"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/pdftext"
	"github.com/amin-jalali/termocode/internal/termimg"
)

// MaxBytes caps each side Describe reads.
const MaxBytes = 32 << 20

// Supported reports whether name is a file Describe can read.
func Supported(name string) bool {
	return isPDF(name) || termimg.IsImagePath(name)
}

func isPDF(name string) bool { return strings.EqualFold(filepath.Ext(name), ".pdf") }

// Describe returns the text of both sides. A nil (empty) side stays ""
// (the file was added or deleted). ok=false when name is not Supported.
func Describe(name string, old, new []byte) (oldText, newText string, ok bool) {
	switch {
	case isPDF(name):
		return describePDF(old), describePDF(new), true
	case termimg.IsImagePath(name):
		o, n := describeImage(old), describeImage(new)
		oldText, newText = o.text(), n.text()
		if newText != "" {
			if line := pixelLine(o, n); line != "" {
				newText += line + "\n"
			}
		}
		return oldText, newText, true
	}
	return "", "", false
}

// imageSide is one decoded side.
type imageSide struct {
	size   int
	format string
	img    image.Image
	note   string // why it has no pixels
}

func describeImage(data []byte) imageSide {
	if len(data) == 0 {
		return imageSide{}
	}
	s := imageSide{size: len(data)}
	if len(data) > MaxBytes {
		s.note = fmt.Sprintf("Too big to compare (over %d MB)", MaxBytes>>20)
		return s
	}
	if _, f, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		s.format = strings.ToUpper(f)
	}
	img, err := termimg.DecodeBytes(data)
	if err != nil {
		s.note = "Could not read the image: " + err.Error()
		return s
	}
	s.img = img
	return s
}

func (s imageSide) text() string {
	if s.size == 0 {
		return ""
	}
	var b strings.Builder
	head := "Image"
	if s.format != "" {
		head += " · " + s.format
	}
	b.WriteString(head + "\n")
	if s.img != nil {
		r := s.img.Bounds()
		fmt.Fprintf(&b, "Size: %d × %d px\n", r.Dx(), r.Dy())
	}
	b.WriteString("File: " + humanSize(s.size) + "\n")
	if s.note != "" {
		b.WriteString(s.note + "\n")
	}
	return b.String()
}

// pixelLine compares the pixels of two same-size sides ("" when there is
// nothing to compare: a side is missing or could not be read).
func pixelLine(o, n imageSide) string {
	if o.img == nil || n.img == nil {
		return ""
	}
	ob, nb := o.img.Bounds(), n.img.Bounds()
	if ob.Dx() != nb.Dx() || ob.Dy() != nb.Dy() {
		return "Pixels: not compared (the size changed)"
	}
	changed := 0
	x0, y0, x1, y1 := ob.Dx(), ob.Dy(), -1, -1
	for y := 0; y < ob.Dy(); y++ {
		for x := 0; x < ob.Dx(); x++ {
			r1, g1, b1, a1 := o.img.At(ob.Min.X+x, ob.Min.Y+y).RGBA()
			r2, g2, b2, a2 := n.img.At(nb.Min.X+x, nb.Min.Y+y).RGBA()
			if r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2 {
				continue
			}
			changed++
			x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
		}
	}
	if changed == 0 {
		return "Pixels: the same"
	}
	total := ob.Dx() * ob.Dy()
	pct := float64(changed) * 100 / float64(total)
	return fmt.Sprintf("Changed pixels: %d of %d (%s%%) in x %d–%d, y %d–%d",
		changed, total, strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.1f", pct), "0"), "."), x0, x1, y0, y1)
}

func describePDF(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	head := "PDF · " + humanSize(len(data)) + "\n"
	if len(data) > MaxBytes {
		return head + fmt.Sprintf("Too big to compare (over %d MB)\n", MaxBytes>>20)
	}
	text, err := pdftext.Extract(data)
	if err != nil {
		return head + "Could not read the PDF: " + err.Error() + "\n"
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return head + "No text found (a scan or an image-only PDF)\n"
	}
	return head + "Text (best effort):\n" + text + "\n"
}

// humanSize is a short byte count ("2.0 KB").
func humanSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
