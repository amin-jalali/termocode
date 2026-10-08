package termimg

import (
	"image"
	"image/color"
)

// resample scales img to w×h with an area-average (box) filter and returns
// non-premultiplied RGBA pixels, row major. Good enough for downscaling,
// which is the common case; upscaling degrades to nearest neighbour.
func resample(img image.Image, w, h int) []color.NRGBA {
	out := make([]color.NRGBA, w*h)
	if w <= 0 || h <= 0 {
		return out
	}
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	for y := 0; y < h; y++ {
		y0 := b.Min.Y + y*sh/h
		y1 := b.Min.Y + (y+1)*sh/h
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < w; x++ {
			x0 := b.Min.X + x*sw/w
			x1 := b.Min.X + (x+1)*sw/w
			if x1 <= x0 {
				x1 = x0 + 1
			}
			// Cap the box so huge downscales stay cheap: sample a
			// regular grid of at most 4×4 source pixels.
			stepX := (x1 - x0 + 3) / 4
			stepY := (y1 - y0 + 3) / 4
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy += stepY {
				for sx := x0; sx < x1; sx += stepX {
					cr, cg, cb, ca := img.At(sx, sy).RGBA() // premultiplied 16-bit
					r += uint64(cr)
					g += uint64(cg)
					bl += uint64(cb)
					a += uint64(ca)
					n++
				}
			}
			if n == 0 || a == 0 {
				out[y*w+x] = color.NRGBA{}
				continue
			}
			// Un-premultiply.
			out[y*w+x] = color.NRGBA{
				R: uint8(r * 0xff / a),
				G: uint8(g * 0xff / a),
				B: uint8(bl * 0xff / a),
				A: uint8(a / n >> 8),
			}
		}
	}
	return out
}

// over blends c onto an opaque background.
func over(c color.NRGBA, bg color.RGBA) color.RGBA {
	a := uint32(c.A)
	return color.RGBA{
		R: uint8((uint32(c.R)*a + uint32(bg.R)*(255-a)) / 255),
		G: uint8((uint32(c.G)*a + uint32(bg.G)*(255-a)) / 255),
		B: uint8((uint32(c.B)*a + uint32(bg.B)*(255-a)) / 255),
		A: 255,
	}
}

func toRGBA(c color.Color) color.RGBA {
	if c == nil {
		return color.RGBA{A: 255}
	}
	r, g, b, _ := c.RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 255}
}
