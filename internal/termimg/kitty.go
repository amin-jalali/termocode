package termimg

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"strings"
)

const kittyChunk = 4096

// KittySeq transmits pngData and displays it over a cols×rows cell area at
// the cursor (a=T). The cursor does not move (C=1) and the terminal sends
// no replies (q=2). Payload is base64 in 4096-byte chunks (m=1 … m=0).
// Placement id 1 is used, so sending again with the same id replaces the
// old placement instead of stacking a second one.
func KittySeq(id uint32, pngData []byte, cols, rows int) string {
	enc := base64.StdEncoding.EncodeToString(pngData)
	var sb strings.Builder
	first := true
	for len(enc) > 0 || first {
		n := len(enc)
		if n > kittyChunk {
			n = kittyChunk
		}
		chunk := enc[:n]
		enc = enc[n:]
		more := 0
		if len(enc) > 0 {
			more = 1
		}
		sb.WriteString("\x1b_G")
		if first {
			fmt.Fprintf(&sb, "a=T,f=100,i=%d,p=1,c=%d,r=%d,C=1,q=2,", id, cols, rows)
			first = false
		}
		fmt.Fprintf(&sb, "m=%d;", more)
		sb.WriteString(chunk)
		sb.WriteString("\x1b\\")
	}
	return sb.String()
}

// KittyDelete removes every placement of image id and frees its data.
func KittyDelete(id uint32) string {
	return fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)
}

// KittyDeleteAll removes all images placed on the visible screen and frees
// their data.
func KittyDeleteAll() string {
	return "\x1b_Ga=d,d=A,q=2\x1b\\"
}

// EncodePNG encodes img as PNG, first scaling it down (aspect kept) so it is
// at most maxW×maxH pixels. maxW/maxH ≤ 0 mean no limit.
func EncodePNG(img image.Image, maxW, maxH int) ([]byte, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if (maxW > 0 && w > maxW) || (maxH > 0 && h > maxH) {
		sx, sy := 1.0, 1.0
		if maxW > 0 && w > maxW {
			sx = float64(maxW) / float64(w)
		}
		if maxH > 0 && h > maxH {
			sy = float64(maxH) / float64(h)
		}
		s := sx
		if sy < s {
			s = sy
		}
		nw, nh := int(float64(w)*s+0.5), int(float64(h)*s+0.5)
		if nw < 1 {
			nw = 1
		}
		if nh < 1 {
			nh = 1
		}
		px := resample(img, nw, nh)
		dst := image.NewNRGBA(image.Rect(0, 0, nw, nh))
		for i, c := range px {
			dst.Pix[i*4+0] = c.R
			dst.Pix[i*4+1] = c.G
			dst.Pix[i*4+2] = c.B
			dst.Pix[i*4+3] = c.A
		}
		img = dst
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
