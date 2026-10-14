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

## Download and upload files

Move files between the workspace and your own computer through the
terminal. No extra port and no second connection.

- **Download**: right-click a file or folder in the explorer and pick
  **Download** (a folder comes as one `.tar.gz`). Or run
  **File: Download to This Computer** from the palette: it takes the
  explorer row when the explorer has focus, else the open file.
- **Upload**: right-click a folder and pick **Upload Here...**, or run
  **File: Upload from This Computer...** (the explorer folder, else the
  workspace root).

A download runs in the background, with a progress toast. Stop it with
**File: Cancel Download**. A file over 50 MB asks first.

| Terminal | Download | Upload |
| --- | --- | --- |
| kitty | yes (kitty file transfer, saved in `~/Downloads`) | yes: type the path on your computer, for example `~/Documents/report.pdf` or a folder |
| iTerm2 | yes (saved in your Downloads folder; up to 256 MB) | yes: iTerm2 opens its file picker |
| WezTerm, Ghostty, GNOME Terminal, others | scp command | scp command |
| any terminal, with [trzsz](https://trzsz.github.io/) | `tsz` (when installed on the server) | `trz` |

- kitty asks you to allow every transfer. Click **Allow** in kitty.
- During an upload termocode hands the terminal over for a moment and
  shows what to do. Press `Ctrl+C` to cancel.
- An upload never replaces a file: when the name is taken, the new file
  gets ` (1)`. Uploads only go into the workspace.
- When the terminal can do neither, a dialog shows a ready `scp` command
  with the real host and path. **Copy Command** puts it in your local
  clipboard (OSC 52). When `tsz` / `trz` are installed on the server, the
  dialog also offers **Use trzsz** (needs trzsz-ssh or a trzsz terminal
  plugin on your computer).
- Over SSH, termocode finds iTerm2 by `LC_TERMINAL` (ssh sends `LC_*` by
  default) and kitty by `TERM=xterm-kitty`. Inside tmux or screen the
  terminal's answers do not reach termocode, so the scp dialog shows.
- Force a protocol with `TERMOCODE_TRANSFER=kitty`, `iterm2` or `off`.
- A persistent session passes the transfer to the attached terminal. A
  download is lost when the terminal disconnects in the middle.

## Security

Sessions listen on a unix socket only, never on the network. The socket
folder (`$XDG_RUNTIME_DIR/termocode`, or `~/.cache/termocode/sessions`) is
private to your user (mode 0700), and the session checks that every client
runs as your user. termocode refuses a socket folder that is a symlink,
belongs to another user or is open to others.

## Troubleshooting

**`termocode` opens a fresh editor, not my session**
:   Check that you are in the same folder: the session belongs to the folder
    you first started termocode in. `termocode sessions` lists the folders.

**Colors or images after I attach from another terminal app**
:   Each attach brings the new terminal's settings: `TERM`, `COLORTERM`,
    `TERMOCODE_COLORS`, `LANG` and the image support (kitty, sixel). The
    editor redraws with them, images too. Only the icon width check and
    the Nerd Font check stay from the first start; if icons look wrong,
    Quit and start again in the new terminal.

**I use mosh**
:   mosh does not set the `SSH_*` variables, so with `auto` termocode
    does not start a session under mosh. Set `persistent_session` to `on`
    on the remote machine.

**The session is gone after I log out**
:   Some systems (systemd with `KillUserProcesses=yes`) end all your
    programs when you log out, the session too. Run
    `loginctl enable-linger` once to keep them. Unsaved text is still in
    the [hot-exit backup](#unsaved-files-after-a-crash).

**A session is stuck**
:   `termocode kill` ends it. If that does not work, the server log is in
    the socket folder (`<key>.log`). The log is removed when a session ends
    normally; it stays when the server crashed.

**"disconnected, the session keeps running"**
:   The session stopped waiting for this terminal: it read nothing for 5
    seconds (a frozen connection). Run `termocode attach` to go back.

**I use tmux already**
:   With the default `auto`, termocode does not start its own session inside
    tmux or screen.
