package termimg

import "testing"

// FuzzProbeReply reads any terminal answer. A cell size is either both
// numbers above zero or none, and the mode names never panic.
func FuzzProbeReply(f *testing.F) {
	for _, s := range []string{
		"\x1b_Gi=31;OK\x1b\\\x1b[6;20;10t\x1b[?62;22c",
		"\x1b_Gi=31;ENOTSUPPORTED:x\x1b\\\x1b[?62;22c",
		"\x1b[6;16;8t\x1b[?64;1;2;4;6;9;15;22c",
		"\x1b[?1;2c", "", "\x1b_Gi=31;OK\x1b\\",
		"\x1b[6;-1;0t\x1b[?\x1b[?4c", "\x1b[?",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _, w, h, _ := parseProbeReply(b)
		if (w == 0) != (h == 0) || w < 0 || h < 0 {
			t.Fatalf("cell size %dx%d", w, h)
		}
		_ = hasSyncOutput(b)
		_ = hasKittyKeys(b)
		_ = ParseMode(string(b))
	})
}

// FuzzFit: a fitted image fits the box, keeps at least one cell, and is
// never empty for a real image and box.
func FuzzFit(f *testing.F) {
	f.Add(800, 600, 80, 24, 10, 20)
	f.Add(1, 100000, 3, 3, 0, 0)
	f.Add(100000, 1, 200, 50, 1, 999999999)
	f.Fuzz(func(t *testing.T, iw, ih, mc, mr, cw, ch int) {
		cols, rows := Fit(iw, ih, mc, mr, cw, ch)
		if iw <= 0 || ih <= 0 || mc <= 0 || mr <= 0 {
			if cols != 0 || rows != 0 {
				t.Fatalf("empty input gave %dx%d", cols, rows)
			}
			return
		}
		if cols < 1 || rows < 1 || cols > mc || rows > mr {
			t.Fatalf("Fit(%d,%d,%d,%d,%d,%d) = %dx%d", iw, ih, mc, mr, cw, ch, cols, rows)
		}
	})
}
