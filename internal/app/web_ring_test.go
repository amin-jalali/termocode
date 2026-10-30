package app

import (
	"strings"
	"testing"
)

// Spec (WEB-7, T48): a topic's resume ring keeps at most 64 deltas and
// 64 KiB of delta data; what falls out moves "lost" up, so a resume from
// before it gets a snap.
func TestRingCaps(t *testing.T) {
	r := &webRing{}
	for i := uint64(1); i <= webRingLen+5; i++ {
		r.add(webRingItem{gseq: i, data: []byte(`{}`)})
	}
	if len(r.items) != webRingLen || r.lost != 5 || r.items[0].gseq != 6 {
		t.Fatalf("count cap: %d items, lost %d", len(r.items), r.lost)
	}
	r = &webRing{}
	big := []byte(`"` + strings.Repeat("x", 20<<10) + `"`)
	for i := uint64(1); i <= 4; i++ {
		r.add(webRingItem{gseq: i, data: big})
	}
	if r.bytes > 64<<10 || len(r.items) != 3 || r.lost != 1 {
		t.Fatalf("byte cap: %d bytes in %d items, lost %d", r.bytes, len(r.items), r.lost)
	}
	r.add(webRingItem{gseq: 5, data: []byte(`"` + strings.Repeat("y", 100<<10) + `"`)})
	if len(r.items) != 1 || r.lost != 4 {
		t.Fatalf("one delta over the cap stays alone: %d items, lost %d", len(r.items), r.lost)
	}
}
