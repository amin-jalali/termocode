package app

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/termimg"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Image preview: opening a PNG / JPEG / GIF / WebP from the explorer, Quick
// Open or a menu shows it in a floating card instead of refusing the file.
//
// Drawing has two layers:
//
//  1. Text: the image as coloured half-blocks (▀ with fg = top pixel,
//     bg = bottom pixel). It is plain cell content, so the overlay
//     compositor, the backdrop dim and every terminal handle it.
//  2. Graphics: when the terminal speaks the kitty graphics protocol or
//     sixel (images setting / startup probe), the real image is drawn on
//     top of the half-blocks via a termimg.Carrier appended to the END of
//     the image's last row, after everything else in the frame. It is
//     left out while the card animates, so it never lands on the wrong
//     cells, and a closed preview deletes its kitty image. The nvim grid
//     is never touched.

// imagePreviewMaxBytes caps the file size we decode.
const imagePreviewMaxBytes = 64 << 20

// Kitty image ids used by termocode.
const (
	kittyIDPreview uint32 = 7101
	kittyIDLogo    uint32 = 7102
)

// imagePreview is the open preview's state.
type imagePreview struct {
	path    string
	img     image.Image
	size    int64
	mod     int64
	loading bool
	err     string
}

type imageLoadedMsg struct {
	path      string
	img       image.Image
	size, mod int64
	err       error
}

var imageCache = termimg.NewCache(16)

// openImagePreview starts decoding path and opens the preview card.
func (m *Model) openImagePreview(path string) tea.Cmd {
	m.imagePrev = &imagePreview{path: path, loading: true}
	m.imageOpen = true
	if imageProtocol() == termimg.Kitty {
		m.kittyUsed = true
	}
	return func() tea.Msg {
		st, err := os.Stat(path)
		if err != nil {
			return imageLoadedMsg{path: path, err: err}
		}
		img, err := termimg.Decode(path, imagePreviewMaxBytes)
		return imageLoadedMsg{path: path, img: img, err: err, size: st.Size(), mod: st.ModTime().UnixNano()}
	}
}

// handleImageMsg routes messages while the preview is open (and the
// decode result). ok=false means "not mine".
func (m *Model) handleImageMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case imageLoadedMsg:
		if m.imagePrev == nil || m.imagePrev.path != msg.path {
			return nil, true
		}
		m.imagePrev.loading = false
		if msg.err != nil {
			m.imagePrev.err = msg.err.Error()
			var c tea.Cmd
			m.toast, c = m.toast.PushDetail(toast.Warn, "Cannot show image", msg.err.Error())
			m.imageOpen = false
			return c, true
		}
		m.imagePrev.img, m.imagePrev.size, m.imagePrev.mod = msg.img, msg.size, msg.mod
		return nil, true
	}
	if !m.imageOpen {
		return nil, false
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q", "enter", "ctrl+c", " ":
			m.imageOpen = false
		}
		return nil, true
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			m.imageOpen = false
		}
		return nil, true
	}
	return nil, false
}

// imageCardLayout is the geometry of the preview card.
type imageCardLayout struct {
	boxW, boxH   int
	cols, rows   int // image area in cells
	imgX, imgY   int // image area offset inside the box
	cellW, cellH int
}

func (m Model) imageLayout() imageCardLayout {
	var l imageCardLayout
	l.cellW, l.cellH = termCaps.CellSize()
	maxCols := m.w*85/100 - 6
	maxRows := m.h*85/100 - 7
	if maxCols < 8 {
		maxCols = 8
	}
	if maxRows < 3 {
		maxRows = 3
	}
	if p := m.imagePrev; p != nil && p.img != nil {
		b := p.img.Bounds()
		cw, ch := l.cellW, l.cellH
		if imageProtocol() == termimg.Blocks || imageProtocol() == termimg.None {
			// Half-blocks: one cell = 1×2 image pixels.
			cw, ch = 1, 2
		}
		l.cols, l.rows = termimg.Fit(b.Dx(), b.Dy(), maxCols, maxRows, cw, ch)
	} else {
		l.cols, l.rows = 24, 3
	}
	inner := l.cols + 4
	if inner < 44 {
		inner = 44
	}
	if inner > m.w-4 {
		inner = m.w - 4
	}
	l.boxW = inner + 2
	// border + header + hairline + blank + image + blank + footer + border
	l.boxH = 1 + 3 + l.rows + 2 + 1
	l.imgX = 1 + (inner-l.cols)/2
	l.imgY = 1 + 3
	return l
}

// renderImageCard draws the card with the half-block image.
func (m Model) renderImageCard(l imageCardLayout) string {
	p := m.imagePrev
	f := theme.Float()
	inner := l.boxW - 2
	bg := lipgloss.NewStyle().Background(f.Bg)
	pad := func(s string) string {
		if w := textWidth(s); w < inner {
			s += bg.Render(strings.Repeat(" ", inner-w))
		}
		return s
	}
	name := filepath.Base(p.path)
	g, c := theme.FileIconFor(name, false, false)
	info := ""
	switch {
	case p.loading:
		info = "loading…"
	case p.img != nil:
		b := p.img.Bounds()
		info = fmt.Sprintf("%d×%d · %s · %s", b.Dx(), b.Dy(), humanSize(p.size), imageProtocol())
	}
	left := bg.Render("  ") + lipgloss.NewStyle().Background(f.Bg).Foreground(theme.LG(c)).Render(g) +
		lipgloss.NewStyle().Background(f.Bg).Foreground(f.Title).Bold(true).Render(" "+name)
	right := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Dim).Render(info) + bg.Render("  ")
	head := left
	if gap := inner - textWidth(left) - textWidth(right); gap > 0 {
		head += bg.Render(strings.Repeat(" ", gap)) + right
	} else if textWidth(left) > inner {
		head = runewidth.Truncate(left, inner, "…")
	}
	rows := []string{pad(head), theme.Hairline(inner, theme.BgFloat), pad("")}

	var imgLines []string
	if p.img != nil {
		imgLines = m.imageBlocks(l)
	}
	lead := bg.Render(strings.Repeat(" ", l.imgX-1))
	for i := 0; i < l.rows; i++ {
		line := ""
		if i < len(imgLines) {
			line = imgLines[i]
		}
		rows = append(rows, pad(lead+line))
	}
	rows = append(rows, pad(""))
	rows = append(rows, pad(bg.Render("  ")+theme.HintBar(f.Bg, "esc", "close")))
	return theme.FloatBox(strings.Join(rows, "\n"), l.boxW)
}

// imageBlocks returns the half-block rendering (cached).
func (m Model) imageBlocks(l imageCardLayout) []string {
	p := m.imagePrev
	key := termimg.CacheKey{Path: p.path, Size: p.size, Cols: l.cols, Rows: l.rows, Proto: termimg.Blocks, Profile: theme.Profile()}
	if e, ok := imageCache.Get(key); ok {
		return e.Lines
	}
	bgc := theme.ToRGB(theme.BgFloat)
	lines := termimg.RenderBlocks(p.img, l.cols, l.rows, color.RGBA{bgc.R, bgc.G, bgc.B, 255}, theme.Profile())
	imageCache.Put(key, termimg.Entry{Lines: lines})
	return lines
}

// graphicsSeq returns the kitty / sixel sequence for img at cols×rows
// (cached; "" when graphics are off).
func graphicsSeq(key termimg.CacheKey, mk func() image.Image, cols, rows int, id uint32) string {
	proto := imageProtocol()
	if proto != termimg.Kitty && proto != termimg.Sixel {
		return ""
	}
	key.Proto = proto
	key.Cols, key.Rows = cols, rows
	if e, ok := imageCache.Get(key); ok && e.Seq != "" {
		return e.Seq
	}
	cw, ch := termCaps.CellSize()
	img := mk()
	var seq string
	switch proto {
	case termimg.Kitty:
		png, err := termimg.EncodePNG(img, cols*cw, rows*ch)
		if err != nil {
			return ""
		}
		seq = termimg.KittySeq(id, png, cols, rows)
	case termimg.Sixel:
		seq = termimg.SixelSeq(img, cols*cw, rows*ch)
	}
	seq = termimg.Wrap(seq, termCaps)
	imageCache.Put(key, termimg.Entry{Seq: seq})
	return seq
}

// imageOverlay composites the preview card and, when the terminal can,
// the real image on top of it.
func (m Model) imageOverlay(base string) string {
	l := m.imageLayout()
	box := m.renderImageCard(l)
	opts := m.overlayOpts()
	frame := modalOverlay(base, box, m.w, m.h, &opts)
	p := m.imagePrev
	if p == nil || p.img == nil || opts.Animating {
		return frame
	}
	key := termimg.CacheKey{Path: p.path, Size: p.size}
	seq := graphicsSeq(key, func() image.Image { return p.img }, l.cols, l.rows, kittyIDPreview)
	if seq == "" {
		return frame
	}
	left := (m.w - l.boxW) / 2
	top := (m.h - l.boxH) / 2
	if left < 0 {
		left = 0
	}
	if top < 0 {
		top = 0
	}
	return placeGraphics(frame, seq, top+l.imgY, left+l.imgX, l.rows)
}

// placeGraphics appends a carrier for seq to the last row of the image
// rectangle (rows tall, top-left at row/col).
func placeGraphics(frame, seq string, row, col, rows int) string {
	lines := strings.Split(frame, "\n")
	last := row + rows - 1
	if row < 0 || last >= len(lines) || rows <= 0 {
		return frame
	}
	hash := termimg.DamageHash(lines[row : last+1])
	lines[last] += termimg.Carrier(seq, row, col, hash)
	return strings.Join(lines, "\n")
}

// cleanupGraphics deletes kitty images that are no longer on show. It is
// appended to the last frame row (a no-op for the terminal when the image
// is already gone).
func (m Model) cleanupGraphics(frame string, previewShown, logoShown bool) string {
	if imageProtocol() != termimg.Kitty {
		return frame
	}
	var del string
	if m.kittyUsed && !previewShown {
		del += termimg.KittyDelete(kittyIDPreview)
	}
	if !logoShown {
		del += termimg.KittyDelete(kittyIDLogo)
	}
	if del == "" {
		return frame
	}
	return frame + termimg.Wrap(del, termCaps)
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
