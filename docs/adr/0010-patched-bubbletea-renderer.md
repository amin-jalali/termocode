# ADR 0010 — A patched copy of Bubble Tea's renderer

Status: **Accepted** (2026-10-15)

## Context
With the editor idle, termocode still used about 3% of a CPU core. A CPU
profile of a minute of idle showed where:

- Bubble Tea v1's renderer ticks 60 times a second for ever. Each tick
  wakes the Go scheduler and, when a frame was written, copies the whole
  frame to compare it with the last one. This was most of the idle cost.
- The prompt guard asked nvim for its mode 10 times a second.
- The git poll started five git processes every 2 seconds.
- The clipboard library's Watch read the X clipboard every second (a new
  X connection each time, and the app that owns the clipboard has to send
  its content).

The last three are our code. The first is inside Bubble Tea and has no
option to turn it off.

## Decision
Keep a copy of Bubble Tea v1.3.10 in `third_party/bubbletea`, used through
a `replace` in go.mod, with a small change in `standard_renderer.go` only
(marked "termocode patch", listed in `third_party/bubbletea/PATCHES.md`):

- the ticker stops when a flush has nothing new, and `write` / `repaint`
  start it again;
- the "same frame?" check does not copy the frame;
- each changed line is measured once, not twice.

CI runs the patch's own test (`TestRendererIdlesAndWakes`).

Our own idle work changed too: the prompt guard polls only while nvim
redraws, the git poll runs one `git status --porcelain=v2 --branch` and
reads the graph only when HEAD moves, and with terminal focus events
(xterm 1004) the clipboard is read when termocode gets focus back, and
git is polled every 10 s while the window is in the background.

## Consequences
- Idle CPU: about 3% → 0% (e2e measure, 60 s idle).
- We must carry the patch when we update Bubble Tea: copy the new
  release into `third_party/bubbletea`, apply the same change, run its
  tests. Bubble Tea v2 has a new renderer; this copy is v1 only.
- Other Bubble Tea behaviour is unchanged: every other file is upstream.
