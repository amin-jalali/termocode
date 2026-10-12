# Remote and SSH

termocode runs well on a remote machine over SSH. When the connection drops,
your editor does not die. Run `termocode` again and you are back where you
were.

## Get your editor back after a drop

1. Your SSH connection drops (Wi-Fi, laptop sleep, VPN).
2. Connect again with `ssh`.
3. Go to the same folder and run `termocode`.

You get the **same** editor: open tabs, unsaved text, undo history, the
terminal panel with its output, and jobs that were running (a build, a
server, `sleep 1000 &`). Nothing restarts.

This works because, over SSH, termocode runs the editor in a background
**session**. The `termocode` you start is only a window into it. When the
window goes away, the session keeps running.

There is one session per folder. `termocode ~/proj` and `termocode ~/other`
are two sessions.

## End a session

- **Quit** (`Ctrl+Q`, or **Quit** in the palette) ends the session for
  real. termocode asks about unsaved files first, as always.
- `termocode kill` ends the session of the current folder from a shell.
  Unsaved files go to the [hot-exit backup](#unsaved-files-after-a-crash).

Closing the terminal window or losing the connection does **not** end it.

**Session: Detach** (command palette, or bind `DetachSession` in
`keymap.json`) gives you back your shell and keeps the session running,
like `tmux detach`. Run `termocode attach` to go back. Outside a session
it only says "Not in a session".

`termocode attach` refuses to run inside a session (for example in its own
terminal panel): the session would show inside itself.

## Commands

| Command | What it does |
|---|---|
| `termocode [dir]` | Open `dir`; attach to its session if one runs |
| `termocode sessions` | List the running sessions |
| `termocode attach [dir]` | Attach to the session of `dir`; error if none runs |
| `termocode kill [dir]` | End the session of `dir` |
| `termocode --no-session` | Run without a session (the old way) |

## Two terminals, one session

If you run `termocode` for a folder whose session is already shown in
another terminal, the new terminal **takes over**. The old one gets back
its shell and shows:

```text
termocode: detached, the session was attached from another terminal
```

This is like `tmux attach -d`. It is handy when you move from your laptop
to your desktop.

## When sessions are on

The `persistent_session` setting (**Preferences: Open Settings**) decides:

| Value | Sessions |
|---|---|
| `auto` (default) | on over SSH, off on your own machine and inside tmux / screen |
| `on` | always |
| `off` | never |

`--no-session` (or `TERMOCODE_NO_SESSION=1`) wins over the setting for one
start. A change applies the next time you start termocode.

Why off on your own machine by default? A local terminal rarely "drops",
and with sessions a second window for the same folder would take the editor
away from the first one. Unsaved files are still safe: see below.

## Unsaved files after a crash

termocode keeps a backup of every file with unsaved changes, a moment after
each change. If termocode is killed, crashes, or loses its terminal without
a session, the next start in the same folder asks:

**Restore unsaved changes?** — **Restore** opens the files with your text
(still unsaved, so you can review and save). **Discard** deletes the
backup. **Later** keeps it and asks again next time.

A clean Quit removes the backup, so you only see the question when
something went wrong. Backups live in
`~/.local/state/termocode/backup/` (or `$XDG_STATE_HOME/termocode/backup/`).
Files that never had a name (new, unsaved buffers) are not backed up.

## Security

Sessions listen on a unix socket only, never on the network. The socket
folder (`$XDG_RUNTIME_DIR/termocode`, or `~/.cache/termocode/sessions`) is
private to your user (mode 0700), and the session checks that every client
runs as your user.

## Troubleshooting

**`termocode` opens a fresh editor, not my session**
:   Check that you are in the same folder: the session belongs to the folder
    you first started termocode in. `termocode sessions` lists the folders.

**Icons or images look wrong after I attach from another terminal app**
:   termocode checks the terminal once, when the session starts. Quit and
    start again in the new terminal.

**A session is stuck**
:   `termocode kill` ends it. If that does not work, the server log is in
    the socket folder (`<key>.log`).

**"disconnected, the session keeps running"**
:   The session stopped waiting for this terminal: it read nothing for 5
    seconds (a frozen connection). Run `termocode attach` to go back.

**I use tmux already**
:   With the default `auto`, termocode does not start its own session inside
    tmux or screen.
