package ansipng

import (
	"errors"
	"image"
	"image/color"
	"image/gif"
	"io"
	"sort"
	"time"
)

// EncodeGIF writes frames as an animated GIF (looping). delays[i] is how
// long frame i stays on screen. Terminal frames use few colours, so one
// shared palette of the 256 most common colours keeps them exact (rarer
// anti-aliasing shades map to the nearest colour). A frame equal to the
// one before is dropped and its time added to it; every other frame
// stores only the rectangle that changed, which keeps a demo small.
func EncodeGIF(w io.Writer, frames []*image.RGBA, delays []time.Duration) error {
	if len(frames) == 0 || len(frames) != len(delays) {
		return errors.New("gif: need one delay per frame")
	}
	pal := gifPalette(frames)
	index := map[color.RGBA]uint8{}
	quant := func(img *image.RGBA) *image.Paletted {
		p := image.NewPaletted(img.Bounds(), pal)
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := img.RGBAAt(x, y)
				i, ok := index[c]
				if !ok {
					i = uint8(pal.Index(c))
					index[c] = i
				}
				p.SetColorIndex(x, y, i)
			}
		}
		return p
	}
	b := frames[0].Bounds()
	out := &gif.GIF{Config: image.Config{ColorModel: pal, Width: b.Dx(), Height: b.Dy()}}
	var prev *image.Paletted
	for i, f := range frames {
		cur := quant(f)
		d := int((delays[i] + 5*time.Millisecond) / (10 * time.Millisecond))
		if prev == nil {
			out.Image = append(out.Image, cur)
			out.Delay = append(out.Delay, d)
			out.Disposal = append(out.Disposal, gif.DisposalNone)
			prev = cur
			continue
		}
		r := changedRect(prev, cur)
		if r.Empty() {
			out.Delay[len(out.Delay)-1] += d
			continue
		}
		out.Image = append(out.Image, cur.SubImage(r).(*image.Paletted))
		out.Delay = append(out.Delay, d)
		out.Disposal = append(out.Disposal, gif.DisposalNone)
		prev = cur
	}
	return gif.EncodeAll(w, out)
}

// gifPalette is the 256 most common colours over all frames.
func gifPalette(frames []*image.RGBA) color.Palette {
	count := map[color.RGBA]int{}
	for _, f := range frames {
		for i := 0; i+3 < len(f.Pix); i += 4 {
			count[color.RGBA{f.Pix[i], f.Pix[i+1], f.Pix[i+2], 0xff}]++
		}
	}
	cs := make([]color.RGBA, 0, len(count))
	for c := range count {
		cs = append(cs, c)
	}
	sort.Slice(cs, func(i, j int) bool {
		if count[cs[i]] != count[cs[j]] {
			return count[cs[i]] > count[cs[j]]
		}
		a, b := cs[i], cs[j]
		return uint32(a.R)<<16|uint32(a.G)<<8|uint32(a.B) < uint32(b.R)<<16|uint32(b.G)<<8|uint32(b.B)
	})
	if len(cs) > 256 {
		cs = cs[:256]
	}
	pal := make(color.Palette, len(cs))
	for i, c := range cs {
		pal[i] = c
	}
	return pal
}

// changedRect is the smallest rectangle holding every pixel that differs.
func changedRect(a, b *image.Paletted) image.Rectangle {
	bd := a.Bounds()
	minX, minY, maxX, maxY := bd.Max.X, bd.Max.Y, bd.Min.X-1, bd.Min.Y-1
	for y := bd.Min.Y; y < bd.Max.Y; y++ {
		ra := a.Pix[(y-bd.Min.Y)*a.Stride:][:bd.Dx()]
		rb := b.Pix[(y-bd.Min.Y)*b.Stride:][:bd.Dx()]
		for x := range ra {
			if ra[x] != rb[x] {
				if x+bd.Min.X < minX {
					minX = x + bd.Min.X
				}
				if x+bd.Min.X > maxX {
					maxX = x + bd.Min.X
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if maxX < minX {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX+1, maxY+1)
}
