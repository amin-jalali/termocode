# Bubble Tea v1.3.10, patched for termocode

This is github.com/charmbracelet/bubbletea v1.3.10 (MIT, see LICENSE),
used through a `replace` in termocode's go.mod. See
docs/adr/0010-patched-bubbletea-renderer.md.

Changed files are marked with "termocode patch". The renderer:

- The frame ticker stops when a flush has nothing new to draw, and
  `write` / `repaint` start it again. Upstream wakes 60 times a second
  for ever, which was most of termocode's CPU while idle.
- The "same frame?" check compares without copying the frame.

`idle_renderer_test.go` is new.

Kitty keyboard protocol (termocode terminal-compat R1):

- `key_csiu.go` (new) parses `CSI code[:alts] ; mods[:event] u`. Keys a
  legacy terminal can also send decode to exactly the same `Key`
  (Ctrl+C, Alt+X, Esc, Ctrl+[ …); the others are `KeyExtended` with
  `Key.Name` ("ctrl+shift+o", "ctrl+.", "ctrl+tab", "shift+enter").
- `key.go`: the `KeyExtended` type, the `Key.Name` field, `String()`
  returns `Name` for it, and `detectOneMsg` tries `detectCSIu` before the
  legacy sequences.
- `WithKittyKeyboard(flags)` (`options.go`, `tea.go`): the standard
  renderer pushes `CSI > flags u` after entering the alt screen and pops
  `CSI < u` before leaving it (`standard_renderer.go`).
- Tests: `key_csiu_test.go`, `kitty_keyboard_test.go`.

All other files are unchanged.

To update: copy the new upstream release here, apply the same change,
run `go test -race ./...` in this directory.
