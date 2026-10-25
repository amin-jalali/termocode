// Package gifkit turns recorded terminal frames into the presentation GIFs
// in assets/gifs: one shared look (dark theme, Nerd Font, 200x50 cells),
// a sidecar .txt with the caption of each scene and its time range, and a
// size check. Frames come from shotkit scenes (the real app model in
// process) or from the real binary in a PTY (the e2e "record" test).
package gifkit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/devtools/ansipng"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Screen size of every GIF, in cells.
const (
	Cols = 200
	Rows = 50
)

// MaxBytes is the size limit of one GIF.
const MaxBytes = 5 << 20

// Frame is one screen (ANSI text, as Bubble Tea prints it or as the VT
// emulator renders it) and how long the GIF shows it.
type Frame struct {
	ANSI string        `json:"ansi,omitempty"`
	File string        `json:"file,omitempty"` // ANSI in a file next to frames.json
	Hold time.Duration `json:"hold_ns"`
}

// Caption is the on-screen caption of a scene; it starts at At.
type Caption struct {
	At   time.Duration `json:"at_ns"`
	Text string        `json:"text"`
}

// Clip is one GIF: frames in order and the captions of its scenes.
type Clip struct {
	Name     string    `json:"name"`
	Note     string    `json:"note,omitempty"` // e.g. "demo models"
	Frames   []Frame   `json:"frames"`
	Captions []Caption `json:"captions"`
}

// Duration is the total time of the clip.
func (c Clip) Duration() time.Duration {
	var d time.Duration
	for _, f := range c.Frames {
		d += f.Hold
	}
	return d
}

// Style is the shared look: the fonts and the cell size.
type Style struct {
	Font, Bold string
	Fallbacks  []string
	Size       float64
}

// Renderer builds the shared renderer (dark theme colours, Nerd Font).
func (s Style) Renderer() (*ansipng.Renderer, error) {
	size := s.Size
	if size == 0 {
		size = 14
	}
	return ansipng.New(ansipng.Options{
		FontRegular: s.Font, FontBold: s.Bold, Size: size, Fallbacks: s.Fallbacks,
		DefaultBG: Hex(theme.Hex(theme.BgEditor)), DefaultFG: Hex(theme.Hex(theme.TextPrimary)),
	})
}

// Hex parses "#rrggbb".
func Hex(h string) color.RGBA {
	var r, g, b uint8
	if _, err := fmt.Sscanf(strings.TrimPrefix(h, "#"), "%02x%02x%02x", &r, &g, &b); err != nil {
		return color.RGBA{0x1c, 0x1c, 0x1c, 0xff}
	}
	return color.RGBA{r, g, b, 0xff}
}

// DefaultFallbacks are the system fonts tried for glyphs the Nerd Font lacks.
func DefaultFallbacks() []string {
	var out []string
	for _, p := range []string{
		"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/noto/NotoSansSymbols2-Regular.ttf",
		"/usr/share/fonts/truetype/noto/NotoSansSymbols-Regular.ttf",
		"/usr/share/fonts/truetype/noto/NotoSansMath-Regular.ttf",
	} {
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// Encode renders the clip and returns the GIF bytes.
func Encode(c Clip, r *ansipng.Renderer) ([]byte, error) {
	if len(c.Frames) == 0 {
		return nil, fmt.Errorf("%s: no frames", c.Name)
	}
	imgs := make([]*image.RGBA, len(c.Frames))
	delays := make([]time.Duration, len(c.Frames))
	for i, f := range c.Frames {
		imgs[i], delays[i] = r.Render(Normalize(f.ANSI), Cols), f.Hold
	}
	var buf bytes.Buffer
	if err := ansipng.EncodeGIF(&buf, imgs, delays); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Write writes dir/<name>.gif and dir/<name>.txt and returns the GIF size.
func Write(c Clip, r *ansipng.Renderer, dir string) (int, error) {
	b, err := Encode(c, r)
	if err != nil {
		return 0, err
	}
	if len(b) > MaxBytes {
		return len(b), fmt.Errorf("%s.gif is %.1f MB, over the %d MB limit", c.Name, float64(len(b))/(1<<20), MaxBytes>>20)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	if err := os.WriteFile(filepath.Join(dir, c.Name+".gif"), b, 0o644); err != nil {
		return 0, err
	}
	return len(b), os.WriteFile(filepath.Join(dir, c.Name+".txt"), []byte(CaptionText(c)), 0o644)
}

// CaptionText is the sidecar file: one line per scene with its time range.
func CaptionText(c Clip) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s.gif - %.1f s, loops. Captions are not burned into the frames.\n", c.Name, c.Duration().Seconds())
	if c.Note != "" {
		fmt.Fprintf(&b, "# Recorded with %s.\n", c.Note)
	}
	total := c.Duration()
	for i, cp := range c.Captions {
		end := total
		if i+1 < len(c.Captions) {
			end = c.Captions[i+1].At
		}
		fmt.Fprintf(&b, "%4.1f-%4.1f s  %s\n", cp.At.Seconds(), end.Seconds(), cp.Text)
	}
	return b.String()
}

// TitleFrame is a full-screen title: lines centred, the first one bold.
func TitleFrame(lines ...string) string {
	bg := Hex(theme.Hex(theme.BgEditor))
	rows := make([]string, Rows)
	blank := fmt.Sprintf("\x1b[48;2;%d;%d;%dm%s\x1b[0m", bg.R, bg.G, bg.B, strings.Repeat(" ", Cols))
	for i := range rows {
		rows[i] = blank
	}
	top := Rows/2 - len(lines)/2 - 1
	for i, l := range lines {
		w := len([]rune(l))
		left := max(0, (Cols-w)/2)
		style := "\x1b[0m"
		if i == 0 {
			style = "\x1b[1m"
		} else {
			fg := Hex(theme.Hex(theme.TextPrimary))
			style = fmt.Sprintf("\x1b[38;2;%d;%d;%dm", fg.R, fg.G, fg.B)
		}
		pad := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", bg.R, bg.G, bg.B)
		rows[top+i*2] = pad + strings.Repeat(" ", left) + style + l + "\x1b[0m" + pad + strings.Repeat(" ", max(0, Cols-left-w)) + "\x1b[0m"
	}
	return strings.Join(rows, "\n")
}

// Load reads frames.json and the .ans files a recording test wrote.
func Load(dir string) (Clip, error) {
	var c Clip
	b, err := os.ReadFile(filepath.Join(dir, "frames.json"))
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	for i, f := range c.Frames {
		if f.File != "" {
			a, err := os.ReadFile(filepath.Join(dir, f.File))
			if err != nil {
				return c, err
			}
			c.Frames[i].ANSI = string(a)
		}
	}
	return c, nil
}

// WithTitle appends the closing title frame of the hero.
func WithTitle(c Clip) Clip {
	at := c.Duration()
	c.Frames = append(c.Frames, Frame{ANSI: TitleFrame("termocode — The AI-Integrated Terminal IDE.", "Your Connection Dies. Your Work Lives On."), Hold: 2500 * time.Millisecond})
	c.Captions = append(c.Captions, Caption{At: at, Text: "termocode — The AI-Integrated Terminal IDE."})
	return c
}

// Normalize makes a screen exactly Rows lines (a terminal emulator drops
// trailing blank rows).
func Normalize(ansi string) string {
	lines := strings.Split(strings.TrimRight(ansi, "\n"), "\n")
	for len(lines) < Rows {
		lines = append(lines, " ") // not empty: the renderer trims trailing newlines
	}
	for i, l := range lines[:Rows] {
		if l == "" {
			lines[i] = " "
		}
	}
	return strings.Join(lines[:Rows], "\n")
}
