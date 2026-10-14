package termimg

import (
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func gradientImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{uint8(x * 255 / w), uint8(y * 255 / h), 128, 255})
		}
	}
	return img
}

func TestParseProbeReply(t *testing.T) {
	cases := []struct {
		name              string
		in                string
		kitty, sixel, da1 bool
		cw, ch            int
	}{
		{"kitty", "\x1b_Gi=31;OK\x1b\\\x1b[6;20;10t\x1b[?62;22c", true, false, true, 10, 20},
		{"kitty error", "\x1b_Gi=31;ENOTSUPPORTED:x\x1b\\\x1b[?62;22c", false, false, true, 0, 0},
		{"sixel", "\x1b[6;16;8t\x1b[?64;1;2;4;6;9;15;22c", false, true, true, 8, 16},
		{"plain", "\x1b[?1;2c", false, false, true, 0, 0},
		{"nothing", "", false, false, false, 0, 0},
		{"partial", "\x1b_Gi=31;OK\x1b\\", true, false, false, 0, 0},
	}
	for _, c := range cases {
		k, s, cw, ch, da1 := parseProbeReply([]byte(c.in))
		if k != c.kitty || s != c.sixel || da1 != c.da1 || cw != c.cw || ch != c.ch {
			t.Errorf("%s: got kitty=%v sixel=%v cell=%dx%d da1=%v", c.name, k, s, cw, ch, da1)
		}
	}
	if !hasDA1([]byte("junk\x1b[?62;4c")) || hasDA1([]byte("\x1b[?62;4")) {
		t.Error("hasDA1")
	}
}

func TestDetectEnv(t *testing.T) {
	cases := []struct {
		env  map[string]string
		p    Protocol
		sure bool
	}{
		{map[string]string{"KITTY_WINDOW_ID": "1"}, Kitty, true},
		{map[string]string{"TERM": "xterm-kitty"}, Kitty, true},
		{map[string]string{"TERM_PROGRAM": "ghostty"}, Kitty, true},
		{map[string]string{"TERM_PROGRAM": "WezTerm"}, Kitty, true},
		{map[string]string{"TERM_PROGRAM": "iTerm.app"}, Sixel, true},
		{map[string]string{"TERM": "foot-extra"}, Sixel, true},
		{map[string]string{"WT_SESSION": "x"}, Sixel, true},
		{map[string]string{"TERM_PROGRAM": "vscode"}, Blocks, true},
		{map[string]string{"TERM": "xterm-256color", "SSH_TTY": "/dev/pts/1"}, Blocks, false},
	}
	for _, c := range cases {
		p, sure := DetectEnv(env(c.env))
		if p != c.p || sure != c.sure {
			t.Errorf("%v: got %v %v", c.env, p, sure)
		}
	}
}

func TestProbeTmuxAndNonTTY(t *testing.T) {
	old := tmuxPassthrough
	defer func() { tmuxPassthrough = old }()

	tmuxPassthrough = func() bool { return false }
	c := Probe(nil, nil, 10*time.Millisecond, env(map[string]string{"TMUX": "x", "TERM_PROGRAM": "ghostty"}))
	if !c.InTmux || c.Passthrough || c.Protocol != Blocks {
		t.Errorf("tmux without passthrough: %+v", c)
	}
	tmuxPassthrough = func() bool { return true }
	c = Probe(nil, nil, 10*time.Millisecond, env(map[string]string{"TMUX": "x", "TERM_PROGRAM": "ghostty"}))
	if !c.Passthrough || c.Protocol != Kitty {
		t.Errorf("tmux with passthrough: %+v", c)
	}
	c = Probe(nil, nil, 10*time.Millisecond, env(map[string]string{"TERM_PROGRAM": "iTerm.app"}))
	if c.Protocol != Sixel {
		t.Errorf("non-tty env fallback: %+v", c)
	}
}

func TestResolve(t *testing.T) {
	kitty := Caps{Protocol: Kitty}
	tmux := Caps{Protocol: Kitty, InTmux: true}
	tmuxPT := Caps{Protocol: Kitty, InTmux: true, Passthrough: true}
	cases := []struct {
		m    Mode
		c    Caps
		want Protocol
	}{
		{ModeOff, kitty, None},
		{ModeBlocks, kitty, Blocks},
		{ModeAuto, kitty, Kitty},
		{ModeAuto, Caps{Protocol: Sixel}, Sixel},
		{ModeAuto, Caps{}, Blocks},
		{ModeAuto, tmux, Blocks},
		{ModeAuto, tmuxPT, Kitty},
		{ModeSixel, Caps{}, Sixel},
		{ModeKitty, tmux, Blocks},
	}
	for _, c := range cases {
		if got := Resolve(c.m, c.c, true); got != c.want {
			t.Errorf("Resolve(%s,%+v) = %v want %v", c.m, c.c, got, c.want)
		}
	}
	if ParseMode(" Kitty ") != ModeKitty || ParseMode("bogus") != ModeAuto || ParseMode("off") != ModeOff {
		t.Error("ParseMode")
	}
}

func TestWrap(t *testing.T) {
	seq := "\x1b_Ga=d\x1b\\"
	if Wrap(seq, Caps{}) != seq || Wrap(seq, Caps{InTmux: true}) != seq {
		t.Error("should not wrap")
	}
	got := Wrap(seq, Caps{InTmux: true, Passthrough: true})
	want := "\x1bPtmux;\x1b\x1b_Ga=d\x1b\x1b\\\x1b\\"
	if got != want {
		t.Errorf("Wrap = %q want %q", got, want)
	}
}

func TestFit(t *testing.T) {
	cases := []struct {
		w, h, mc, mr, cw, ch int
		c, r                 int
	}{
		{100, 100, 40, 40, 10, 20, 10, 5},    // small image: not scaled up
		{1000, 1000, 40, 40, 10, 20, 40, 20}, // width limited
		{1000, 4000, 80, 20, 10, 20, 10, 20}, // height limited
		{200, 100, 50, 50, 0, 0, 50, 13},     // default 1:2 cells, width limited
		{0, 10, 10, 10, 1, 2, 0, 0},
	}
	for _, c := range cases {
		gc, gr := Fit(c.w, c.h, c.mc, c.mr, c.cw, c.ch)
		if gc != c.c || gr != c.r {
			t.Errorf("Fit(%d,%d,%d,%d,%d,%d) = %d,%d want %d,%d", c.w, c.h, c.mc, c.mr, c.cw, c.ch, gc, gr, c.c, c.r)
		}
	}
}

func TestRenderBlocksWidth(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 37, 23))
	for y := 0; y < 23; y++ {
		for x := 0; x < 37; x++ {
			img.Set(x, y, color.NRGBA{uint8(x * 7), uint8(y * 11), 90, uint8(255 - x)})
		}
	}
	for _, p := range []termenv.Profile{termenv.TrueColor, termenv.ANSI256, termenv.ANSI, termenv.Ascii} {
		lines := RenderBlocks(img, 17, 6, color.RGBA{30, 30, 46, 255}, p)
		if len(lines) != 6 {
			t.Fatalf("profile %v: %d lines", p, len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w != 17 {
				t.Errorf("profile %v line %d width %d", p, i, w)
			}
			if p == termenv.Ascii && strings.Contains(l, "\x1b") {
				t.Errorf("ascii profile emitted escape: %q", l)
			}
		}
	}
	if RenderBlocks(img, 0, 3, nil, termenv.TrueColor) != nil {
		t.Error("zero cols should give nil")
	}
}

func TestSixelSeq(t *testing.T) {
	img := gradientImage(64, 40)
	// Transparent corner.
	img.Set(0, 0, color.NRGBA{})
	s := SixelSeq(img, 50, 30)
	if !strings.HasPrefix(s, "\x1bP0;1;0q\"1;1;50;30") || !strings.HasSuffix(s, "\x1b\\") {
		t.Fatalf("bad sixel framing: %q…", s[:min(40, len(s))])
	}
	if n := strings.Count(s, "-"); n != 5 { // 30 rows / 6 per band
		t.Errorf("bands = %d want 5", n)
	}
	// Palette ≤ 256 entries: count "#i;2;" definitions.
	if n := len(regexp.MustCompile(`#\d+;2;`).FindAllString(s, -1)); n > 256 || n == 0 {
		t.Errorf("palette size %d", n)
	}
	// Body must stay inside the sixel alphabet + control chars.
	body := s[strings.Index(s, "q")+1 : len(s)-2]
	for _, r := range body {
		if r < 0x20 || r > 0x7e {
			t.Fatalf("non-printable %q in sixel body", r)
		}
	}
}

func TestKittySeqChunking(t *testing.T) {
	data := make([]byte, 10000) // base64 → 13336 bytes → 4 chunks
	s := KittySeq(7, data, 20, 10)
	chunks := strings.Count(s, "\x1b_G")
	if chunks != 4 {
		t.Fatalf("chunks = %d want 4", chunks)
	}
	if !strings.HasPrefix(s, "\x1b_Ga=T,f=100,i=7,p=1,c=20,r=10,C=1,q=2,m=1;") {
		t.Errorf("first chunk header: %q", s[:60])
	}
	if strings.Count(s, "m=1;") != 3 || strings.Count(s, "m=0;") != 1 {
		t.Error("m flags wrong")
	}
	for _, part := range strings.Split(s, "\x1b\\") {
		if part == "" {
			continue
		}
		payload := part[strings.Index(part, ";")+1:]
		if len(payload) > kittyChunk {
			t.Errorf("chunk payload %d > %d", len(payload), kittyChunk)
		}
	}
	if KittyDelete(7) != "\x1b_Ga=d,d=I,i=7,q=2\x1b\\" {
		t.Error("KittyDelete")
	}
	small := KittySeq(1, []byte("x"), 1, 1)
	if strings.Count(small, "\x1b_G") != 1 || !strings.Contains(small, "m=0;") {
		t.Errorf("single chunk: %q", small)
	}
}

func TestEncodePNGDownscales(t *testing.T) {
	b, err := EncodePNG(gradientImage(400, 200), 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 100 || img.Bounds().Dy() != 50 {
		t.Errorf("size %v", img.Bounds())
	}
}

func TestDecode(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "a.png")
	f, _ := os.Create(pngPath)
	_ = png.Encode(f, gradientImage(8, 4))
	_ = f.Close()
	gifPath := filepath.Join(dir, "b.GIF")
	f, _ = os.Create(gifPath)
	pal := image.NewPaletted(image.Rect(0, 0, 5, 3), []color.Color{color.Black, color.White})
	_ = gif.EncodeAll(f, &gif.GIF{Image: []*image.Paletted{pal, pal}, Delay: []int{0, 0}})
	_ = f.Close()

	img, err := Decode(pngPath, 0)
	if err != nil || img.Bounds().Dx() != 8 {
		t.Fatalf("png: %v %v", err, img)
	}
	img, err = Decode(gifPath, 0)
	if err != nil || img.Bounds().Dx() != 5 {
		t.Fatalf("gif: %v", err)
	}
	if _, err := Decode(pngPath, 10); err == nil {
		t.Error("size limit not enforced")
	}
	txt := filepath.Join(dir, "c.png")
	_ = os.WriteFile(txt, []byte("not an image"), 0o644)
	if _, err := Decode(txt, 0); err == nil {
		t.Error("garbage decoded")
	}
	if !IsImagePath("x/Y.WEBP") || !IsImagePath("a.jpeg") || IsImagePath("a.svg") {
		t.Error("IsImagePath")
	}
}

// Bubble Tea's standard renderer runs ansi.Truncate(line, width, "") on every
// line. A carrier appended after a full-width row must survive untouched and
// add no width.
func TestCarrierSurvivesTruncate(t *testing.T) {
	row := "\x1b[48;2;1;2;3m" + strings.Repeat("x", 40) + "\x1b[0m"
	seqs := []string{
		KittySeq(3, make([]byte, 5000), 10, 5),
		SixelSeq(gradientImage(20, 12), 20, 12),
		KittyDelete(3),
		"",
	}
	for i, s := range seqs {
		line := row + Carrier(s, 4, 7, 12345)
		if w := ansi.StringWidth(line); w != 40 {
			t.Errorf("seq %d: width %d want 40", i, w)
		}
		if got := ansi.Truncate(line, 40, ""); got != line {
			t.Errorf("seq %d: Truncate changed the carrier line", i)
		}
	}
	c := Carrier("X", 4, 7, 5)
	if c != "\x1b7\x1b[6d\x1b[5;8HX\x1b8" {
		t.Errorf("Carrier = %q", c)
	}
	if DamageHash([]string{"a", "b"}) == DamageHash([]string{"a", "c"}) {
		t.Error("DamageHash collision on trivial change")
	}
}

func TestCache(t *testing.T) {
	c := NewCache(2)
	k := func(p string) CacheKey { return CacheKey{Path: p, Cols: 1, Rows: 1} }
	c.Put(k("a"), Entry{Seq: "a"})
	c.Put(k("b"), Entry{Seq: "b"})
	if _, ok := c.Get(k("a")); !ok { // a becomes most recent
		t.Fatal("a missing")
	}
	c.Put(k("c"), Entry{Seq: "c"}) // evicts b
	if _, ok := c.Get(k("b")); ok {
		t.Error("b should be evicted")
	}
	if e, ok := c.Get(k("a")); !ok || e.Seq != "a" {
		t.Error("a evicted")
	}
	if c.Len() != 2 {
		t.Errorf("len %d", c.Len())
	}
}

// TestCacheBytesBound: big renderings are evicted to stay under
// maxCacheBytes, the newest one always stays, and Bytes follows.
func TestCacheBytesBound(t *testing.T) {
	c := NewCache(16)
	k := func(p string) CacheKey { return CacheKey{Path: p, Cols: 1, Rows: 1} }
	big := strings.Repeat("x", maxCacheBytes/3+1)
	for _, p := range []string{"a", "b", "c", "d"} {
		c.Put(k(p), Entry{Seq: big, Lines: []string{"ab"}})
		if c.Bytes() > maxCacheBytes {
			t.Fatalf("after %s: %d bytes cached, limit %d", p, c.Bytes(), maxCacheBytes)
		}
	}
	if _, ok := c.Get(k("d")); !ok {
		t.Error("newest entry evicted")
	}
	if _, ok := c.Get(k("a")); ok {
		t.Error("oldest entry kept past the byte limit")
	}
	huge := strings.Repeat("y", maxCacheBytes+1)
	c.Put(k("e"), Entry{Seq: huge})
	if c.Len() != 1 || c.Bytes() != len(huge) {
		t.Errorf("one oversized entry: len %d bytes %d", c.Len(), c.Bytes())
	}
	c.Put(k("e"), Entry{Seq: "small"})
	if c.Bytes() != len("small") {
		t.Errorf("replacing an entry: bytes %d", c.Bytes())
	}
}

func TestProtocolString(t *testing.T) {
	if Kitty.String() != "kitty" || None.String() != "off" || Blocks.String() != "blocks" || Sixel.String() != "sixel" {
		t.Error("String")
	}
	if w, h := (Caps{}).CellSize(); w != 10 || h != 20 {
		t.Error("CellSize default")
	}
}
