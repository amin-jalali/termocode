// Package termimg draws raster images in the terminal.
//
// Three back ends are supported, best first:
//
//   - Kitty graphics protocol (kitty, Ghostty, WezTerm): the PNG is sent in
//     an APC sequence and placed over a cols×rows cell rectangle.
//   - Sixel (iTerm2, foot, WezTerm, Windows Terminal, xterm -ti vt340): the
//     image is quantized to ≤256 colors and sent as a DCS sequence.
//   - Half blocks: every cell shows two vertical pixels with "▀" (fg = top,
//     bg = bottom). Plain text, so it works everywhere, survives overlays and
//     is the base layer the other two are drawn on top of.
//
// Detection happens once, before Bubble Tea owns the TTY (Probe): a kitty
// graphics query, a cell-size query (CSI 16 t) and DA1 (which lists "4" when
// sixel is supported). Inside tmux graphics are only used when
// allow-passthrough is on; the sequences are then wrapped (Wrap).
//
// # The carrier trick
//
// Bubble Tea's renderer writes whole lines and skips lines that did not
// change since the last flush. Graphics are therefore embedded into the frame
// as a zero-width "carrier" (Carrier) appended to the END of the last row of
// the image rectangle: rows are written top-down, so all text of the
// rectangle is already on screen when the carrier draws the image — text can
// not erase sixel pixels written earlier in the same flush. The carrier saves
// the cursor (DECSC), moves to the rectangle's top-left, emits the image and
// restores the cursor (DECRC), so the renderer's cursor bookkeeping is
// unaffected. A harmless VPA whose argument is derived from a hash of the
// rectangle's rows makes the carrier string change whenever any row of the
// rectangle changed, so the renderer rewrites the carrier (and redraws the
// image) after the rectangle was damaged.
//
// The renderer calls ansi.Truncate(line, width, "") on every line; Truncate
// returns the line unchanged when its visible width fits, and StringWidth
// ignores APC/DCS bodies, so a carrier after a full-width row survives (see
// TestCarrierSurvivesTruncate).
package termimg
