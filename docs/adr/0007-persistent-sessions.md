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
  SIGTERM to the UI (unsaved buffers go to the hot-exit backup).
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
set `persistent_session` to `on`. A crash or a closed window without a
session is covered by the hot-exit backup of unsaved files
(`internal/app/hot_exit.go`): nvim writes every modified buffer to
`~/.local/state/termocode/backup/<key>/<pid>/` a moment after each change
(our nvim runs with `-n`, so swap files were not an option, and a backup
written at signal time comes too late when nvim gets the same hangup);
the next start offers Restore / Discard / Later.

## Consequences
- **+** A dropped SSH connection loses nothing: unsaved text, undo, terminal
  panel, running jobs, debug sessions, AI chat.
- **+** No new external tool; one more Go dependency (`creack/pty`).
- **−** Two extra processes per session (client + server) and one more hop
  for every byte (a plain copy over a local socket).
- **+** Each attach brings the new terminal's capabilities (added
  2026-10-12). The hello frame carries, after the size, a JSON object
  with the client's terminal variables (`session.CapsEnvKeys`: `TERM`,
  `COLORTERM`, `TERM_PROGRAM`, `LANG` / `LC_*`, `NO_COLOR`,
  `TERMOCODE_COLORS`, …) and, when it attaches to a running UI, its own
  graphics probe (kitty / sixel, cell size, tmux). An old server reads
  only the size; a new server treats a size-only hello (old client) as
  "no caps". The server writes the caps to `<key>.caps.json` (0600, path
  in `TERMOCODE_SESSION_CAPS`) before RepaintSignal; the UI copies the
  variables into its environment and re-applies the colour profile,
  graphics protocol, icons and animations, then redraws every line.
  Images are drawn again with the redraw (their carrier is part of the
  frame). Each such attach has a number (`gen` in the caps); the UI starts
  every frame with a private APC mark for it (`session.AttachedSeq`),
  and until that mark comes the server sends the new client no output,
  because it was drawn for the old terminal. After 2 s without the mark
  the server sends the output anyway and asks for one more redraw. Not covered: the Kitty keyboard protocol (Bubble Tea does not
  use it; pushed flags are replayed with the other modes) and OSC 52,
  which is always sent.
- **−** The icon width probe and the Nerd Font check still run only at
  the first attach.
- **−** Open Recent Workspace inside a session keeps the session keyed to
  the folder it started in.
- **−** mosh does not set `SSH_CONNECTION` / `SSH_CLIENT` / `SSH_TTY`, so
  `auto` stays off under mosh (mosh keeps the connection alive itself, but
  not across a reboot of the client or `mosh-server` timeouts). Users set
  `persistent_session` to `on`.
- **−** With systemd-logind `KillUserProcesses=yes`, logging out kills the
  session server with everything else; `loginctl enable-linger` keeps it.
  The hot-exit backup still saves unsaved text.
- Unix only (Linux, macOS); other systems run the UI directly.
