# The web panel

The web panel is a second screen for the termocode you already run. It
opens in a browser tab. It shows the same chats, files, terminals and git
state as the terminal, and you can work in both at the same time. Nothing
is copied: the page shows what termocode shows, and what you do in the
page happens in termocode.

The web panel is **experimental**. It runs only when you open it.

!!! warning "Anyone who controls the browser tab can run any command as you"
    The page can type into your terminals and answer the AI's cards.
    Open the link only in a browser you trust, and stop the panel when
    you are done.

## Open it

In termocode, press `F1` and type `web`. **Web: Open in Browser** is
first. Press `Enter`. That is all: the panel starts and opens in your
browser. You do not need to change a setting or restart termocode.

Or, in a terminal in the folder where termocode runs:

```sh
termocode web                # open it in the browser now
termocode web status         # is it on, and until when?
termocode web stop
```

- **Web: Open in Browser** (also named **Web: Start Panel**) starts the
  panel and opens it. If it runs already, it opens a new link.
- **Web: Copy Link** copies a new link while the panel runs.
- **Web: Stop Panel** stops it. Every browser is disconnected at once.
  The web chip in the status bar does the same when you click it.

The panel listens only on this computer (`127.0.0.1`).

If no browser opens (for example, on a computer with no desktop),
termocode copies the link for you and shows it in a toast. Paste it into
a browser. Click the toast to copy it again.

When a script runs `termocode web` (not you, typing in a terminal),
termocode shows a card on the laptop first. **Not now** is first and is
what `Enter` and `Esc` do. `termocode web --yes` skips the card only when
your `config.json` has `"web_panel": "on"`.

To turn the panel off completely, add to your own `config.json` (see
[Config files](../reference/config-files.md)):

```json
{ "web_panel": "off" }
```

Settings apply when you save `config.json`. No restart is needed.

The link works **once**, for **5 minutes**. The page then gets its own
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
| **Files** | Browse the workspace, read files, change lines and send them. Each row's **⋯** makes a new file or folder, renames, deletes, downloads a file or uploads into a folder. |
| **Term** | Watch the terminals and type into them, with a key bar for Esc, Ctrl, Alt and arrows. |
| **Git** | See changes and diffs, stage and unstage, commit. The **⇅** button pushes, pulls, stashes and switches branch. |
| **More** | Search, tasks, runs, databases, containers, the MCP server, settings (read only), the theme, text direction and language. |

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
- The panel must be turned on in the server's `config.json`:
  `"web_panel": "on"`.
- The second `ssh` cannot ask for a password. Use an SSH key or
  `ssh-agent`.
- After a reconnect the forward comes back, and the open tab connects
  again by itself.
- When you leave `termocode ssh`, the forward ends.

## Use it on a phone

The page is made for a phone: big buttons, a tab bar for your thumb, and
a key bar for the terminal. The easy way to reach it from your phone is
your own [Tailscale](https://tailscale.com) network (your *tailnet*).

### Turn on tailnet mode

You need Tailscale on this computer and on the phone, both logged in to
the same tailnet, and **HTTPS certificates** turned on for the tailnet
(in the Tailscale admin console, on the DNS page). Then add to
`config.json`:

```json
{ "web_listen": "tailnet" }
```

**Web: Open in Browser** now says that your tailnet devices can reach the
panel. When it starts, termocode runs `tailscale serve` for the panel,
and the panel gets an address like `https://box.tail1234.ts.net/`. The
toast and `termocode web status` show it.

- The panel itself still listens only on `127.0.0.1`. Tailscale passes
  the requests on to it.
- It is **never public**. If the port is under Tailscale Funnel (open to
  the internet), the panel does not start, and it stops at once when
  Funnel is turned on later. Run `tailscale funnel off`.
- When the panel stops, `tailscale serve` stops with it. Nothing is left
  behind in your Tailscale settings.
- On Linux, Tailscale may refuse a normal user. Run once:
  `sudo tailscale set --operator=$USER`.

### Pair your phone

1. In termocode, run **Web: Pair a Device…**. A QR code shows. It works
   **once**, for **2 minutes**.
2. Scan it with the phone's camera. The page opens. Tap **Pair this
   device with the laptop**.
3. The phone shows a **6-digit code**. termocode shows a card with the
   phone's name and a code. Pair only if the two codes are the same.
   **Not now** is first; pick **Pair as viewer** or **Pair with
   control**.
4. The phone asks you to **save a passkey** (Face ID, fingerprint or the
   phone's PIN). Then the workspace opens.

If someone else scanned the code before you, their code is on the card,
not yours: pick **Not now**. Your phone then says the link was used;
make a new one.

### Sign in again

A paired phone needs no new link. Open the panel's address and tap
**Sign in with passkey**. There are no passwords.

### A sign-in code as well (optional)

For a second step after the passkey, run **Web: Set Up Sign-in Code…**.
It shows a QR code for an authenticator app on your phone, and the key
as text. The key is kept in your
[secret store](../reference/config-files.md), which must be unlocked when a phone signs
in. Then add `"web_require_totp": true` to `config.json`. A phone now
types the 6-digit code after its passkey. A code works once; after 5
wrong codes it waits 5 minutes. The one-time link from this computer
does not need it.

### Viewers and control

A **viewer** sees your chats, files, terminals and git state, but cannot
type, answer cards or change anything. The page shows **View only** at
the top.

To get control, a paired viewer taps **Ask for control**. The phone
asks for its passkey, and termocode shows a card: **Give control to
iPhone · Safari?** **Not now** is first. Only your answer on the laptop,
within 5 minutes of the passkey, gives control. The phone then comes
back with control by itself.

To take control back, open **Web: Devices…**, pick the device and choose
**Make viewer**.

### Devices

**Web: Devices…** lists the paired devices with their role, and whether
they are connected now. Pick one to see it, make it a viewer, or
**Remove device**. A removed device is disconnected at once and its
passkey works nowhere; it must pair again to come back.

### Share with another person

To let someone else watch, run **Web: Share as Viewer…**:

1. Share this computer with them in Tailscale first (in the admin
   console, **Share** on this machine). They need Tailscale too.
2. **Copy link** and send it to them. It works **once**, for **10
   minutes**.
3. They open it and read you the **6-digit code** they see. If it is the
   same as on your card, pick **Let them watch**.
4. They save a passkey and can watch. They can never get control. Remove
   them in **Web: Devices…** when you are done.

### Without Tailscale

The panel can also be reached through an SSH app on the phone that
forwards the same port number to this computer. Then open a fresh link
(**Web: Copy Link**) in the phone's browser within 5 minutes.

## How it keeps you safe

In simple words:

- **Only this computer.** The panel listens on `127.0.0.1`, never on the
  network. Over SSH it uses a private socket instead.
- **Only you.** On Linux, a connection from another user on the same
  computer is refused before anything is read.
- **A one-time link.** The secret is in the part of the link after `#`,
  which the browser never sends anywhere. It is never in a program's
  command line or in a log. It is on the screen only when no browser
  opens, and never on another screen. It works once, for 5 minutes.
- **No cookies.** Each tab has its own key. Close the tab and the key is
  gone.
- **A strict page.** The page loads nothing from the internet and runs no
  code from your files or from the AI. Links from the chat open only
  after you confirm.
- **Safe first.** On every card, **Deny** (or Reject, Not now) is first
  and has the focus. `Enter` and `Esc` never do the risky thing, and
  the risky button works only after a short moment on screen. The page
  never offers "Always allow".
- **Big things need the laptop.** Discarding changes, closing a
  terminal, deleting a file, replacing a file with an upload, push,
  pull, stash, switching branch, stopping or removing a container and
  any SQL that may change data ask on the laptop first.
- **Passkeys, no passwords.** A paired device signs in with a passkey
  that needs your face, finger or PIN. A copied passkey is noticed and
  blocked. A removed device is out at once.
- **Your yes, on the laptop.** Pairing a device, letting someone watch
  and giving control each need a card on the laptop, with **Not now**
  first. The phone and the card show the same code, so you know which
  phone you let in.
- **Never public.** In tailnet mode the panel refuses to run under
  Tailscale Funnel.
- **Passwords stay on the laptop.** Git signs in on the laptop. The
  database view shows no host, user or password, and it hides values of
  columns such as `password` or `token`. Files that look like they hold
  secrets (such as `.env` or keys) are never downloaded.
- **The AI cannot open the door.** The AI can never start the panel,
  pair a device, give control or change its settings. It cannot run or
  type `termocode web` either.
- **A record.** termocode keeps an audit log of web starts, stops and
  logins in its config folder (`web-audit.jsonl`).

## Limits

- At most **4** browsers at once (`web_max_viewers`, up to 8).
- A paste into a terminal is at most **1 MiB**.
- Long files open read-only (the first 2 000 lines). Files that look like
  they hold secrets (such as `.env`) open only on the laptop.
- Search stops at 500 matches or 10 seconds.
- An upload or a download is at most **256 MB**.
- Container logs show the last **64 KiB**; database results show at
  most 1 000 rows.
- No access from another computer on your network, other than through
  SSH or your own tailnet.
- At most 32 paired devices.

## Settings

| Setting | Default | What it does |
|---|---|---|
| `web_panel` | `auto` | `auto`: open it on this computer. `on`: also over SSH, and a script's `--yes`. `off`: never. |
| `web_port` | `0` | A fixed port on `127.0.0.1` (1024..65535); `0` picks a free one. |
| `web_idle_minutes` | `15` | Stop after this many minutes without use (1..120). |
| `web_max_hours` | `12` | Stop this many hours after the start (1..24). |
| `web_open_browser` | `true` | Open the link in this computer's browser on start. |
| `web_max_viewers` | `4` | Browsers at once (1..8). |
| `web_listen` | `loopback` | `tailnet`: also reach it from your tailnet devices. |
| `web_require_totp` | `false` | A paired device also types a code from an authenticator app. |

These settings live only in your own `config.json`. A project's settings
cannot change them.

## Troubleshooting

**`termocode web` says no termocode runs here**
:   Start termocode in this folder first. Check that `config.json` does
    not say `"web_panel": "off"`.

**"This link has expired or was already used"**
:   Each link works once, for 5 minutes. Use **Web: Open in Browser** for
    a new one.

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

**"Tailscale is not installed here" / "not on a tailnet"**
:   Install Tailscale and run `tailscale up`, or set `"web_listen":
    "loopback"`.

**"Your tailnet gives this computer no HTTPS name"**
:   Turn on HTTPS certificates in the Tailscale admin console (DNS page).

**"This port is public on the internet (Tailscale Funnel)"**
:   Run `tailscale funnel off`, then start the panel again.

**"Tailscale Serve already uses port 443 on this computer"**
:   Something else is served on port 443. See `tailscale serve status`.

**"This device was removed"**
:   The device was removed on the laptop. Pair it again.

**"This passkey looks copied"**
:   termocode saw the passkey's counter go back, so it blocked the
    device. Remove it in **Web: Devices…** and pair it again.

**The browser did not open**
:   termocode copied the link and shows it in a toast. Paste it into the
    browser yourself within 5 minutes. Click the toast to copy it again.
