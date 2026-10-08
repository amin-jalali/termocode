package termimg

import (
	"bufio"
	"fmt"
	"image"
	_ "image/gif" // register decoders
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp"
)

// IsImagePath reports whether path has an extension Decode understands.
func IsImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}

// Decode reads an image file (PNG, JPEG, GIF first frame, WebP). Files over
// maxBytes are refused (maxBytes <= 0 means no limit).
func Decode(path string, maxBytes int64) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if maxBytes > 0 {
		st, err := f.Stat()
		if err != nil {
			return nil, err
		}
		if st.Size() > maxBytes {
			return nil, fmt.Errorf("image too large (%d bytes, limit %d)", st.Size(), maxBytes)
		}
	}
	img, _, err := image.Decode(bufio.NewReader(f))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, fmt.Errorf("empty image")
	}
	return img, nil
}

// Fit returns the largest cols×rows cell rectangle that shows an imgW×imgH
// image inside maxCols×maxRows without changing its aspect ratio. cellW and
// cellH are the cell size in pixels (≤0 means the usual 1:2 cell). The
// image is never scaled up beyond its own pixel size in cells (at least 1×1).
func Fit(imgW, imgH, maxCols, maxRows, cellW, cellH int) (cols, rows int) {
	if imgW <= 0 || imgH <= 0 || maxCols <= 0 || maxRows <= 0 {
		return 0, 0
	}
	if cellW <= 0 || cellH <= 0 {
		cellW, cellH = 1, 2
	}
	// Image size measured in cells (fractional).
	wc := float64(imgW) / float64(cellW)
	hc := float64(imgH) / float64(cellH)
	scale := 1.0
	if s := float64(maxCols) / wc; s < scale {
		scale = s
	}
	if s := float64(maxRows) / hc; s < scale {
		scale = s
	}
	cols = int(wc*scale + 0.5)
	rows = int(hc*scale + 0.5)
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	if cols > maxCols {
		cols = maxCols
	}
	if rows > maxRows {
		rows = maxRows
	}
	return cols, rows
}
