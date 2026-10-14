package app

import (
	"image/color"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/termimg"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Markdown preview images: a local raster image that stands alone in a
// paragraph (![alt](path)) is drawn inline as coloured half-blocks. The
// preview is a scrolling text view, so we use the text layer only (no
// kitty / sixel placement — those cannot follow the scroll offset
// safely). Remote URLs and files that cannot be decoded keep the
// renderer's "[image: alt]" placeholder.

const mdImageMaxRows = 18

// markdownImage is the mdview image hook for a document in baseDir.
func markdownImage(baseDir string) func(dest, alt string, width int) []string {
	return func(dest, _ string, width int) []string {
		if strings.Contains(dest, "://") || !termimg.IsImagePath(dest) {
			return nil
		}
		p := dest
		if !filepath.IsAbs(p) {
			p = filepath.Join(baseDir, p)
		}
		img, err := termimg.Decode(p, imagePreviewMaxBytes)
		if err != nil {
			return nil
		}
		bgc := theme.ToRGB(theme.BgEditor)
		bg := color.RGBA{bgc.R, bgc.G, bgc.B, 255}
		b := img.Bounds()
		cols, rows := termimg.Fit(b.Dx(), b.Dy(), width, mdImageMaxRows, 1, 2)
		return termimg.RenderBlocks(img, cols, rows, bg, theme.Profile())
	}
}
