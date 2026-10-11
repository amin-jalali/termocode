# ADR 0007 — Persistent sessions: the UI runs behind a socket, on over SSH

Status: **Accepted** (2026-10-11)

## Context
Over SSH a dropped connection sends SIGHUP to everything in the terminal:
termocode, nvim, the terminal panel's shells and their jobs die, and unsaved
edits are lost. Users asked for "reconnect and get the exact last state".

Options:

1. Tell users to run termocode inside tmux / screen. Works, but it is an
   extra tool to learn, and tmux changes keys, mouse and colours.
2. Save and restore state (open files, unsaved text) on hangup. Running jobs,
   terminal output and undo trees cannot be restored this way.
3. Keep the process alive, like dtach / abduco: run the UI inside a PTY that
   a small background process owns, and let the foreground process be a
   thin client.

## Decision
Option 3, built in (`internal/session`, `cmd/termocode/session_unix.go`):

- `termocode` (the client) connects to a unix socket
  `$XDG_RUNTIME_DIR/termocode/<key>.sock` (fallback
  `~/.cache/termocode/sessions`), key = hash of the workspace folder. With
  no server it starts one (`termocode __session-server <dir>`, own session
  via `setsid`). The server starts the real UI (`termocode --no-session`)
  on a PTY when the first client says hello.
- The client relays keys, output and the window size. When it dies, the
  server keeps the PTY and drops the UI's output.
- On attach the server replays the terminal modes the UI has set (alt
  screen, mouse, bracketed paste, cursor shape, colours, kitty keyboard)
  — `Modes` follows them in the output stream, so run-time changes count —
  resizes the PTY and sends SIGUSR1; the UI then sends a size message and
  `tea.ClearScreen`, so Bubble Tea redraws every line.
- A second client takes over; the first one gets its terminal reset and a
  short message (like `tmux attach -d`).
- Quit ends the UI, and the server exits with it. `termocode kill` sends
  SIGTERM to the UI.
- Security: socket dir 0700, socket 0600, peer uid checked
  (SO_PEERCRED / LOCAL_PEERCRED), no network listener.
- Setting `persistent_session`: `auto` (default), `on`, `off`; flag
  `--no-session`.

**Default:** `auto` = on over SSH (`SSH_CONNECTION`, `SSH_CLIENT` or
`SSH_TTY` set), off locally and inside tmux / screen. Locally a terminal
rarely "drops", and a session there has costs users would notice: running
`termocode` in a second window for the same folder takes the editor away
from the first window, and the UI keeps the environment of the first
start. Inside tmux the process already survives. Users who want it locally
set `persistent_session` to `on`.

## Consequences
- **+** A dropped SSH connection loses nothing: unsaved text, undo, terminal
  panel, running jobs, debug sessions, AI chat.
- **+** No new external tool; one more Go dependency (`creack/pty`).
- **−** Two extra processes per session (client + server) and one more hop
  for every byte (a plain copy over a local socket).
- **−** The UI probes the terminal (icons, images) once, at the first
  attach. A client in a different terminal program gets the first one's
  capabilities.
- **−** Graphics drawn with kitty / sixel are not repainted on attach until
  the UI draws them again.
- **−** Open Recent Workspace inside a session keeps the session keyed to
  the folder it started in.
- Unix only (Linux, macOS); other systems run the UI directly.
