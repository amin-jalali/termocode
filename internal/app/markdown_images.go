package app

import (
	"fmt"
	"image/color"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/amin-jalali/termocode/internal/termimg"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Markdown preview images: local raster images referenced with ![alt](path)
// are drawn inline as coloured half-blocks. The preview is a scrolling
// text view, so we use the text layer only (no kitty / sixel placement —
// those cannot follow the scroll offset safely). Remote URLs and missing
// files keep glamour's normal "Image: alt → url" line.

var mdImageRe = regexp.MustCompile(`!\[([^\]]*)\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)`)

const mdImageMaxRows = 18

// mdImageToken is the placeholder glamour renders as a plain word.
func mdImageToken(i int) string { return fmt.Sprintf("TCIMGPLACEHOLDER%dX", i) }

// extractMarkdownImages swaps local image references for placeholder
// paragraphs and returns the image paths in placeholder order.
func extractMarkdownImages(content, baseDir string) (string, []string) {
	var paths []string
	out := mdImageRe.ReplaceAllStringFunc(content, func(s string) string {
		m := mdImageRe.FindStringSubmatch(s)
		src := m[2]
		if strings.Contains(src, "://") || !termimg.IsImagePath(src) {
			return s
		}
		if !filepath.IsAbs(src) {
			src = filepath.Join(baseDir, src)
		}
		paths = append(paths, src)
		return "\n\n" + mdImageToken(len(paths)-1) + "\n\n"
	})
	return out, paths
}

// injectMarkdownImages replaces placeholder lines in rendered output with
// the images. A file that cannot be decoded becomes a dim "[image: …]" line.
func injectMarkdownImages(rendered string, paths []string, width int) string {
	if len(paths) == 0 {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	out := make([]string, 0, len(lines))
	bgc := theme.ToRGB(theme.BgEditor)
	bg := color.RGBA{bgc.R, bgc.G, bgc.B, 255}
	indent := theme.Bg(theme.BgEditor).Render("  ")
	for _, line := range lines {
		idx := -1
		for i := range paths {
			if strings.Contains(line, mdImageToken(i)) {
				idx = i
				break
			}
		}
		if idx < 0 {
			out = append(out, line)
			continue
		}
		img, err := termimg.Decode(paths[idx], imagePreviewMaxBytes)
		if err != nil {
			out = append(out, indent+theme.FgBg(theme.TextDim, theme.BgEditor).Render("[image: "+filepath.Base(paths[idx])+"]"))
			continue
		}
		b := img.Bounds()
		cols, rows := termimg.Fit(b.Dx(), b.Dy(), width-4, mdImageMaxRows, 1, 2)
		for _, l := range termimg.RenderBlocks(img, cols, rows, bg, theme.Profile()) {
			out = append(out, indent+l)
		}
	}
	return strings.Join(out, "\n")
}
