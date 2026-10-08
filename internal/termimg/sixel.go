package termimg

import (
	"image"
	"image/color"
	"sort"
	"strconv"
	"strings"
)

// SixelSeq scales img to pxW×pxH pixels, quantizes it to at most 256 colors
// (median cut) and encodes it as a sixel DCS sequence. Pixels with alpha <
// 50% stay transparent (P2=1), so the cell background shows through.
func SixelSeq(img image.Image, pxW, pxH int) string {
	if img == nil || pxW <= 0 || pxH <= 0 {
		return ""
	}
	px := resample(img, pxW, pxH)
	pal := medianCut(px, 256)
	idx := mapToPalette(px, pal)

	var sb strings.Builder
	sb.WriteString("\x1bP0;1;0q")
	sb.WriteString("\"1;1;")
	sb.WriteString(strconv.Itoa(pxW))
	sb.WriteByte(';')
	sb.WriteString(strconv.Itoa(pxH))
	for i, c := range pal {
		sb.WriteByte('#')
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(";2;")
		sb.WriteString(strconv.Itoa(int(c.R) * 100 / 255))
		sb.WriteByte(';')
		sb.WriteString(strconv.Itoa(int(c.G) * 100 / 255))
		sb.WriteByte(';')
		sb.WriteString(strconv.Itoa(int(c.B) * 100 / 255))
	}
	row := make([]byte, pxW)
	used := make([]bool, len(pal))
	for band := 0; band < pxH; band += 6 {
		for i := range used {
			used[i] = false
		}
		for y := band; y < band+6 && y < pxH; y++ {
			for x := 0; x < pxW; x++ {
				if p := idx[y*pxW+x]; p >= 0 {
					used[p] = true
				}
			}
		}
		firstColor := true
		for ci := range pal {
			if !used[ci] {
				continue
			}
			for x := 0; x < pxW; x++ {
				var bits byte
				for k := 0; k < 6; k++ {
					y := band + k
					if y < pxH && idx[y*pxW+x] == ci {
						bits |= 1 << k
					}
				}
				row[x] = 63 + bits
			}
			if !firstColor {
				sb.WriteByte('$')
			}
			firstColor = false
			sb.WriteByte('#')
			sb.WriteString(strconv.Itoa(ci))
			writeRLE(&sb, row)
		}
		sb.WriteByte('-')
	}
	sb.WriteString("\x1b\\")
	return sb.String()
}

// writeRLE writes sixel characters with "!n" run-length encoding, dropping
// trailing empty sixels.
func writeRLE(sb *strings.Builder, row []byte) {
	end := len(row)
	for end > 0 && row[end-1] == 63 {
		end--
	}
	for i := 0; i < end; {
		j := i + 1
		for j < end && row[j] == row[i] {
			j++
		}
		n := j - i
		if n > 3 {
			sb.WriteByte('!')
			sb.WriteString(strconv.Itoa(n))
			sb.WriteByte(row[i])
		} else {
			for k := 0; k < n; k++ {
				sb.WriteByte(row[i])
			}
		}
		i = j
	}
}

type colorCount struct {
	c color.RGBA
	n int
}

// medianCut builds a palette of at most max colors from the opaque pixels.
func medianCut(px []color.NRGBA, max int) []color.RGBA {
	hist := map[uint16]int{}
	for _, p := range px {
		if p.A < 128 {
			continue
		}
		hist[key15(p.R, p.G, p.B)]++
	}
	if len(hist) == 0 {
		return []color.RGBA{{A: 255}}
	}
	all := make([]colorCount, 0, len(hist))
	for k, n := range hist {
		all = append(all, colorCount{c: unkey15(k), n: n})
	}
	if len(all) <= max {
		out := make([]color.RGBA, len(all))
		for i, cc := range all {
			out[i] = cc.c
		}
		return out
	}
	boxes := [][]colorCount{all}
	for len(boxes) < max {
		// Split the box with the widest channel range (weighted by size).
		best, bestScore := -1, -1
		for i, b := range boxes {
			if len(b) < 2 {
				continue
			}
			_, r := widestChannel(b)
			score := r * len(b)
			if score > bestScore {
				best, bestScore = i, score
			}
		}
		if best < 0 {
			break
		}
		b := boxes[best]
		ch, _ := widestChannel(b)
		sort.Slice(b, func(i, j int) bool { return chanOf(b[i].c, ch) < chanOf(b[j].c, ch) })
		// Split at the pixel-count median.
		total := 0
		for _, cc := range b {
			total += cc.n
		}
		acc, cut := 0, 1
		for i, cc := range b {
			acc += cc.n
			if acc >= total/2 {
				cut = i + 1
				break
			}
		}
		if cut >= len(b) {
			cut = len(b) - 1
		}
		boxes[best] = b[:cut]
		boxes = append(boxes, b[cut:])
	}
	out := make([]color.RGBA, 0, len(boxes))
	for _, b := range boxes {
		var r, g, bl, n int
		for _, cc := range b {
			r += int(cc.c.R) * cc.n
			g += int(cc.c.G) * cc.n
			bl += int(cc.c.B) * cc.n
			n += cc.n
		}
		if n == 0 {
			continue
		}
		out = append(out, color.RGBA{uint8(r / n), uint8(g / n), uint8(bl / n), 255})
	}
	return out
}

func widestChannel(b []colorCount) (ch, rng int) {
	lo := [3]int{255, 255, 255}
	hi := [3]int{}
	for _, cc := range b {
		v := [3]int{int(cc.c.R), int(cc.c.G), int(cc.c.B)}
		for k := 0; k < 3; k++ {
			if v[k] < lo[k] {
				lo[k] = v[k]
			}
			if v[k] > hi[k] {
				hi[k] = v[k]
			}
		}
	}
	for k := 0; k < 3; k++ {
		if hi[k]-lo[k] > rng {
			ch, rng = k, hi[k]-lo[k]
		}
	}
	return
}

func chanOf(c color.RGBA, ch int) uint8 {
	switch ch {
	case 0:
		return c.R
	case 1:
		return c.G
	}
	return c.B
}

func key15(r, g, b uint8) uint16 {
	return uint16(r>>3)<<10 | uint16(g>>3)<<5 | uint16(b>>3)
}

func unkey15(k uint16) color.RGBA {
	r := uint8(k>>10&31)<<3 | 4
	g := uint8(k>>5&31)<<3 | 4
	b := uint8(k&31)<<3 | 4
	return color.RGBA{r, g, b, 255}
}

// mapToPalette returns the palette index per pixel (-1 = transparent).
func mapToPalette(px []color.NRGBA, pal []color.RGBA) []int {
	cache := map[uint16]int{}
	out := make([]int, len(px))
	for i, p := range px {
		if p.A < 128 {
			out[i] = -1
			continue
		}
		k := key15(p.R, p.G, p.B)
		if v, ok := cache[k]; ok {
			out[i] = v
			continue
		}
		best, bestD := 0, 1<<31-1
		for j, c := range pal {
			dr := int(p.R) - int(c.R)
			dg := int(p.G) - int(c.G)
			db := int(p.B) - int(c.B)
			d := 2*dr*dr + 4*dg*dg + 3*db*db
			if d < bestD {
				best, bestD = j, d
			}
		}
		cache[k] = best
		out[i] = best
	}
	return out
}
