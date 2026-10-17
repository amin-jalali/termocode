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

Frame pacing for slow links (termocode Group S2):

- `pacing.go` (new): `WithFramePacing(window)`. Each frame ends with a
  status query (`CSI 5 n`); the renderer holds frames back while more
  than `window` bytes are unanswered, so a slow SSH line never queues
  seconds of frames (run-on after a held key). Terminals that never
  answer are not paced; a lost answer is forgiven after 1 s.
- `standard_renderer.go`: `flushFrame(query)`; `listen` waits for the
  pacer. `tea.go`: the answers (`CSI 0 n`) are taken out of the message
  stream; `shutdown` and `exec.go` wait briefly for the last answers so
  they never reach the shell or a child program.
- Test: `pacing_test.go`.

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
- `key.go` sequences: `\x1b[3;2~` is Shift+Delete (upstream had it as
  Alt+Insert); Alt+Insert is `\x1b[2;3~`. `key_sequences.go`: modified
  special keys the table does not list (Ctrl+F12, Ctrl+Shift+PgUp …) are
  named by `detectModifiedSpecial` instead of dropped.
- `ParseInput` (`key_parse.go`): decodes a burst of input bytes into
  messages, so termocode's tests can check every shortcut end to end.
- Tests: `key_csiu_test.go`, `kitty_keyboard_test.go`, `key_parse_test.go`.

Terminal answers are not keys (termocode terminal-compat R3):

- `key_strings.go` (new): a complete escape string in the input (APC
  `ESC _ … ST`, DCS `ESC P … ST`, OSC `ESC ] … BEL/ST`, PM, SOS) is one
  `escapeStringMsg`, not Alt+_ plus typed text. A late answer to the
  startup probe (kitty graphics `ESC _ Gi=31;OK ESC \`) can no longer
  type into the editor. Without a terminator the bytes stay keys.
- `key.go`: `detectOneMsg` tries `detectEscapeString` first after paste.
- Test: `key_strings_test.go`.

Input reader and mouse modes (termocode terminal-compat R6):

- `input_hold.go` (new) replaces upstream's `readAnsiInputs` (removed
  from `key.go`). An incomplete escape sequence at the end of a short
  read (lone ESC, `ESC [` with only parameter bytes, `ESC O`, an escape
  string without its terminator) is held until the next read or
  `holdWait` (50 ms), so a split SGR mouse report is one `MouseMsg`, not
  typed text. A lone Esc key is delivered after that wait. Reads happen
  one at a time on request (a goroutine that never reads ahead), so a
  released terminal loses no input.
- In one read, a run of plain pointer motion (no button) is delivered
  as its last event; clicks, drags and wheel are never merged.
- `tea.go`: `EnableMouseCellMotion` turns 1003 off first and
  `EnableMouseAllMotion` turns 1002 off first, so switching modes never
  leaves both on (`screen_test.go` expectations updated, new
  `mouse_switch` case).
- Test: `split_input_test.go`.

Synchronized output (termocode terminal-compat R10):

- `WithSynchronizedOutput()` (`options.go`, `tea.go`): `flushFrame`
  (`standard_renderer.go`) writes each frame between `CSI ? 2026 h` and
  `CSI ? 2026 l`, in the same single write, so the terminal shows it at
  once (images in the frame included). With frame pacing the status
  query goes after the end mark. `kill` writes the end mark too, so a
  frame is never left open. termocode turns it on only when the terminal
  answered DECRQM for mode 2026 (or `TERMOCODE_SYNC=on`).
- Test: `sync_output_test.go`.

All other files are unchanged.

To update: copy the new upstream release here, apply the same change,
run `go test -race ./...` in this directory.
