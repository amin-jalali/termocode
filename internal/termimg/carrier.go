package termimg

import (
	"hash/fnv"
	"strconv"
)

// Carrier wraps a graphics sequence so it can be appended to the END of the
// last row of the image rectangle in a Bubble Tea frame (see the package
// doc). row and col are the 0-based top-left cell of the rectangle.
//
//	DECSC                save cursor (and SGR state)
//	CSI <1+hash%97> d    VPA: harmless (the CUP below wins) but makes the
//	                     carrier text change when the rectangle's rows
//	                     changed, so the renderer's line diff rewrites it
//	CSI row+1;col+1 H    go to the rectangle's top-left
//	seq                  the image
//	DECRC                restore cursor, so the renderer's bookkeeping holds
//
// An empty seq still yields a valid (image-less) carrier, which is how a
// kitty delete or "nothing to draw" state is expressed.
func Carrier(seq string, row, col int, damageHash uint32) string {
	return "\x1b7" +
		"\x1b[" + strconv.Itoa(1+int(damageHash%97)) + "d" +
		"\x1b[" + strconv.Itoa(row+1) + ";" + strconv.Itoa(col+1) + "H" +
		seq +
		"\x1b8"
}

// CarrierBefore is the same carrier for placing at the START of the row
// just below the rectangle. Not needed with the current Bubble Tea renderer
// (a carrier after a full-width row survives ansi.Truncate), kept as the
// fallback should that change.
func CarrierBefore(seq string, row, col int, damageHash uint32) string {
	return Carrier(seq, row, col, damageHash)
}

// DamageHash hashes the rows of the image rectangle; pass it to Carrier.
func DamageHash(rows []string) uint32 {
	h := fnv.New32a()
	for _, r := range rows {
		_, _ = h.Write([]byte(r))
		_, _ = h.Write([]byte{'\n'})
	}
	return h.Sum32()
}
