# Bubble Tea v1.3.10, patched for termocode

This is github.com/charmbracelet/bubbletea v1.3.10 (MIT, see LICENSE),
used through a `replace` in termocode's go.mod. See
docs/adr/0010-patched-bubbletea-renderer.md.

Only `standard_renderer.go` is changed (look for "termocode patch"):

- The frame ticker stops when a flush has nothing new to draw, and
  `write` / `repaint` start it again. Upstream wakes 60 times a second
  for ever, which was most of termocode's CPU while idle.
- The "same frame?" check compares without copying the frame.

`idle_renderer_test.go` is new. All other files are unchanged.

To update: copy the new upstream release here, apply the same change,
run `go test -race ./...` in this directory.
