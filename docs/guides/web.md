# The web panel

The web panel is a second screen for the termocode you already run. It
opens in a browser tab. It shows the same chats, files, terminals and git
state as the terminal, and you can work in both at the same time. Nothing
is copied: the page shows what termocode shows, and what you do in the
page happens in termocode.

The web panel is **experimental** and **off** by default.

!!! warning "Anyone who controls the browser tab can run any command as you"
    The page can type into your terminals and answer the AI's cards.
    Open the link only in a browser you trust, and stop the panel when
    you are done.

## Turn it on

Add one line to your own `config.json` (see
[Config files](../reference/config-files.md)):

```json
{ "web_panel": "on" }
```

When you save, termocode opens a private socket for the panel. Nothing
listens on the network yet. The panel itself starts only when you ask.

## Start and stop

In termocode:

- **Web: Start Panel** shows a card first. It says what the browser can
  do. Pick **Open in browser** or **Copy link**. **Not now** is first and
  is what `Enter` and `Esc` do.
- **Web: Copy Link** copies a new link while the panel runs.
- **Web: Stop Panel** stops it. Every browser is disconnected at once.
  The web chip in the status bar does the same when you click it.

From a shell, in the folder where termocode runs:

```sh
termocode web start          # asks on the laptop first
termocode web start --yes    # you are at the laptop: no question
termocode web status         # is it on, and until when?
termocode web stop
```

The link works **once**, for **60 seconds**. The page then gets its own
key, which lives only in that tab. A new tab needs a new link.

The panel also stops by itself:

- after **15 minutes** with no use from any browser (`web_idle_minutes`),
- **12 hours** after it started (`web_max_hours`),
- when you quit termocode.

Your work does not stop with it. Chats, terminals and runs go on in
termocode.

## What you can do in the page

| Tab | What it does |
|---|---|
| **Chat** | Read and write in the AI chat. Answers stream in. Answer the AI's cards. |
| **Files** | Browse the workspace, read files, change lines and send them. |
| **Term** | Watch the terminals and type into them, with a key bar for Esc, Ctrl, Alt and arrows. |
| **Git** | See changes and diffs, stage and unstage, commit. |
| **More** | Search, tasks, runs, the theme, text direction and language. |

On a phone the tabs are at the bottom. On a wider screen the files and
the chat sit side by side.

## Use it from a remote machine: `termocode ssh --web`

When termocode runs on a server, open its panel on your own computer:

```sh
termocode ssh --web --dir ~/proj me@server
```

This is [`termocode ssh`](remote-ssh.md#reconnect-by-itself-termocode-ssh)
with one more step. termocode on the server starts the panel on a private
socket. A second `ssh`, with your own options and keys, forwards a port on
your computer to that socket. The link then opens in your browser here.

- The server opens **no network port**. Everything goes through your SSH
  connection.
- The panel must be on in the server's `config.json`.
- The second `ssh` cannot ask for a password. Use an SSH key or
  `ssh-agent`.
- After a reconnect the forward comes back, and the open tab connects
  again by itself.
- When you leave `termocode ssh`, the forward ends.

## Use it on a phone

The page is made for a phone: big buttons, a tab bar for your thumb, and
a key bar for the terminal. In this version the panel listens only on the
computer that runs it (`127.0.0.1`). A phone can reach it only through an
SSH app on the phone that forwards the same port number to that
computer. Then open a fresh link (**Web: Copy Link**) in the phone's
browser within 60 seconds.

## How it keeps you safe

In simple words:

- **Only this computer.** The panel listens on `127.0.0.1`, never on the
  network. Over SSH it uses a private socket instead.
- **Only you.** On Linux, a connection from another user on the same
  computer is refused before anything is read.
- **A one-time link.** The secret is in the part of the link after `#`,
  which the browser never sends anywhere. It is never in a program's
  command line, in a log or on the screen. It works once, for 60 seconds.
- **No cookies.** Each tab has its own key. Close the tab and the key is
  gone.
- **A strict page.** The page loads nothing from the internet and runs no
  code from your files or from the AI. Links from the chat open only
  after you confirm.
- **Safe first.** On every card, **Deny** (or Reject, Not now) is first
  and has the focus. `Enter` and `Esc` never do the risky thing, and
  the risky button works only after a short moment on screen. The page
  never offers "Always allow".
- **Big things need the laptop.** Discarding changes or closing a
  terminal asks on the laptop first.
- **The AI cannot open the door.** The AI can never start the panel or
  change its settings.
- **A record.** termocode keeps an audit log of web starts, stops and
  logins in its config folder (`web-audit.jsonl`).

## Limits

- At most **4** browsers at once (`web_max_viewers`, up to 8).
- A paste into a terminal is at most **1 MiB**.
- Long files open read-only (the first 2 000 lines). Files that look like
  they hold secrets (such as `.env`) open only on the laptop.
- Search stops at 500 matches or 10 seconds.
- No file upload or download from the page yet.
- No access from another computer on your network yet, other than
  through SSH.

## Settings

| Setting | Default | What it does |
|---|---|---|
| `web_panel` | `off` | `on` lets you start the panel. |
| `web_port` | `0` | A fixed port on `127.0.0.1` (1024..65535); `0` picks a free one. |
| `web_idle_minutes` | `15` | Stop after this many minutes without use (1..120). |
| `web_max_hours` | `12` | Stop this many hours after the start (1..24). |
| `web_open_browser` | `true` | Open the link in this computer's browser on start. |
| `web_max_viewers` | `4` | Browsers at once (1..8). |

These settings live only in your own `config.json`. A project's settings
cannot turn the panel on.

## Troubleshooting

**`termocode web start` says no termocode runs here**
:   Start termocode in this folder first, and check `"web_panel": "on"`
    in `config.json`.

**"This link has expired or was already used"**
:   Each link works once, for 60 seconds. Use **Web: Copy Link** for a
    new one.

**The page says "Offline"**
:   The page lost its connection and tries again by itself. Your work goes
    on in termocode. If the panel was stopped, start it again.

**"The web session stopped after 15 minutes without typing"**
:   The panel stops when no one uses it. Start it again, or raise
    `web_idle_minutes`.

**"Too many browsers are open on this session"**
:   Close a tab you do not use, or raise `web_max_viewers`.

**`termocode ssh --web`: "SSH could not forward the web panel"**
:   The server may not allow socket forwarding (`AllowStreamLocalForwarding`
    in its `sshd_config`), or `ssh` wanted a password. Use a key or
    `ssh-agent`, or ask the server's admin.

**`termocode ssh --web`: "no web panel: The web panel is off on the far computer"**
:   Turn it on in the server's `config.json`: `"web_panel": "on"`.

**The browser did not open**
:   Use **Web: Copy Link** and paste the link into the browser yourself
    within 60 seconds.
