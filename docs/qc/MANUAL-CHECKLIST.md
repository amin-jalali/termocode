# Manual checklist — things the automated tests can not prove

Every item here **has** automated tests, but they run against a fake
terminal, a fake clipboard or a fake AI. These checks need a real terminal,
a real desktop or a human eye. Run them after a release build, top to
bottom; mark each box ✅ / ❌ and write what you saw under ❌ items.

**Setup**

1. `go build -o termocode ./cmd/termocode` in the repo.
2. Close **all** termocode windows first, then start a fresh `./termocode`
   in the repo folder (old processes keep old code).
3. Window about 200×50 or larger unless a step says otherwise.
4. Note: terminal + version, OS, desktop (Wayland/X11), nvim version.

| Field | Value |
|---|---|
| Date | |
| Commit (`git log -1 --oneline`) | |
| Terminal / version | |
| Desktop (Wayland / X11) | |
| nvim version | |

---

## A. Speed and feel

**A1 — Held arrow key stops at once.** Open a long file (e.g.
`internal/app/view.go`), click into the editor, hold ↓ for 3 s, let go.
Expected: the cursor stops at once (no extra lines move after release).
Same with the AI chat open (Alt+A) next to the editor. ☐

**A2 — Mouse wheel stops at once.** Scroll fast with the wheel for 3 s in
the editor, stop. Expected: scrolling stops at once. ☐

**A3 — Idle CPU.** Leave termocode idle for a minute with a file and the AI
chat open. In another terminal: `top -p $(pgrep -x termocode)`.
Expected: termocode under ~5 % CPU when idle. ☐

**A4 — Panel drag.** Drag the bottom terminal panel's top bar up and down.
Expected: the bar and the panel content move together, no lag or
half-drawn bar. ☐

**A5 — Startup.** Start termocode. Expected: the screen appears in well
under a second, no blank wait. ☐

## B. Look

**B1 — Focus marks.** Click the explorer, then the editor, then the
terminal panel. Expected: the active tab's ▎ mark is bright only when the
editor has the keys; the line under EXPLORER is blue while the explorer
has focus. ☐

**B2 — AI panel edge.** Open the AI chat. Expected: a thin line separates
it from the editor. ☐

**B3 — Hover labels.** Rest the mouse 0.5 s on: an activity bar icon, the
⋮ menu, the AI panel's + ≡ ⚐ × buttons, the terminal panel buttons, a
status bar item. Expected: a small label with the name (and shortcut);
it hides when the mouse moves, on click or on a key. ☐

**B4 — Markdown preview.** Open a `.md` file (e.g. `docs/features.md`),
press F7. Expected: centred column, clear headings with rules, lists,
code blocks with a frame and colours, tables with borders; no broken
blocks. Try a light theme too (Settings → theme). ☐

**B5 — Icons.** Settings → Appearance → icons = `auto`. Expected in GNOME
Terminal with Ubuntu Mono: Unicode icons, no boxes with hex numbers.
Then set your GNOME profile font to "JetBrainsMono Nerd Font", restart
GNOME Terminal fully (close all its windows), start termocode: icons
become Nerd icons. ☐

**B6 — Dashboard logo.** Settings → dashboard_logo = `image` in kitty or a
sixel terminal: the logo image is not cut off. Back to `text`. ☐

## C. Clipboard and copy

**C1 — Copy from the AI chat, drag.** Ask the AI something. Drag-select
part of the answer. Expected: highlight while dragging, toast "Copied N
chars". Paste with Ctrl+V into **a normal desktop app** (gedit, browser):
the same text, no borders or colour codes. ☐

**C2 — Double / triple click.** Double-click a word, triple-click a line
in the answer, paste each. ☐

**C3 — Copy buttons.** "⧉ Copy" on an answer header, the ⧉ next to your
question, a code block's Copy, and the right-click menu items. Paste each
in a desktop app. ☐

**C4 — Ctrl+C.** With a selection: copies. Without a selection while the
AI is answering: stops the answer. ☐

**C5 — No false "Copied".** If C1 fails (nothing pasted), the toast must
not have said "Copied" — it should say what to install (wl-clipboard /
xclip). Note what it said. ☐

## D. Files in and out

**D1 — Local download.** Explorer → right-click a file → Download.
Expected: no dialog; toast "Saved to ~/Downloads/…"; the file is there;
clicking the toast opens the file manager on it. Download the same file
again: it is saved as "name (1)". ☐

**D2 — Local folder download.** Download a folder: a copy appears in
~/Downloads. ☐

**D3 — Local upload.** Right-click a folder → Upload Here… Expected: the
GNOME file picker opens; pick two files; they appear in that folder in
the explorer; a taken name gets " (1)". Cancel the picker: nothing
happens. ☐

**D4 — Over SSH, GNOME Terminal.** `ssh` into a machine with termocode,
run it, Download a file. Expected: a dialog starting with "To get …,
run this command on your own computer", one-line `scp` command in a
frame; Copy Command copies it (paste it locally to check) or warns. ☐

**D5 — kitty over SSH** (if you use kitty). Download: kitty asks to allow,
the file lands in ~/Downloads, same bytes (`cmp`). Upload Here…: type a
local path, the file appears on the server. ☐

**D6 — iTerm2 over SSH** (if you use macOS). Download: the file lands in
Downloads. Upload Here…: iTerm2's picker opens, files arrive. A big file
(~100 MB) too. ☐

**D7 — Drop a file into the AI chat.** Drag a file from the file manager
onto the terminal while the AI input has focus. Expected: a chip with the
file name, not the path as text. An image (png) too: the AI can describe
it (Claude/OpenAI providers). ☐

**D8 — Save as / export.** On a code block in the answer: Save as… →
default name, file written and opened. Palette "AI: Export Chat as
Markdown" → `ai-chat-<date>.md` opens. ☐

## E. AI drives the IDE (real model)

Use your real provider (claude-opus). Say each line in the chat; check the
screen, not only the answer.

**E1** "create notes/hello.md with a title" → file in the explorer.
**E2** "open it" → it opens in the editor, no confirm.
**E3** "rename it to hello2.md" → tab and explorer show hello2.md; the old
name is gone everywhere (tabs, recent files).
**E4** "delete it" → asks first; tab closes; explorer row gone.
**E5** "switch to the light theme" → asks (settings); colours change; the
setting survives a restart.
**E6** "make the sidebar wider" → sidebar width changes.
**E7** "run `go test ./internal/clock/` in the terminal and tell me the
result" → asks (run); the visible terminal runs it; the AI reports
"ok".
**E8** "show me the last 5 commits" and "who wrote line 10 of
internal/clock/clock.go" → correct log / blame.
**E9** Make an edit to a file, then "discard my change in that file" →
asks with **no** "Always allow"; the change is gone in the buffer.
**E10** "rename the function Since in internal/clock to Elapsed" (on a
throw-away branch!) → every use changes, undo works in the editor.
**E11** "run the tests and tell me which fail" → Testing view runs; the
AI lists failures with file:line.
**E12** "quit termocode" → refused, nothing happens.
**E13** Long task: the AI may pause at 25 steps with a clear note;
"continue" goes on.

| E1 | E2 | E3 | E4 | E5 | E6 | E7 | E8 | E9 | E10 | E11 | E12 | E13 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ |

**E14 — Debugger** (needs a dlv that matches your Go version; dlv 1.25.2
can not read variables of Go 1.25+ programs — update with
`go install github.com/go-delve/delve/cmd/dlv@latest`). Small Go program:
"set a breakpoint on line N and start debugging" → stops there; "what is
x?" → the value; "step over" → next line; "stop". ☐

**E15 — Inline suggestions.** Type normal code in a Go file for a minute.
Expected: no ghost text that invents a new function after a closing `}`
or on an empty top-level line; suggestions finish the current line. ☐

## F. Terminal panel and session

**F1 — exit in one shell tab.** Open 2 terminal tabs, type `exit` in one.
Expected: the other tab stays, panel stays open. ☐

**F2 — `:qa`.** Type `:qa` in the editor (no unsaved files). Expected:
termocode quits, no "nvim exited" dialog. With an unsaved file: nvim
refuses as usual. ☐

**F3 — Open Workspace keeps unsaved text.** Type in a file without
saving, Open Workspace → another folder, then open the first folder
again. Expected: "Restore" for the unsaved text. ☐

**F4 — SSH reattach.** Over SSH: start termocode, close the SSH window
(drop), ssh again, run `termocode` in the same folder. Expected: the same
session comes back, full screen drawn (not blank). ☐

**F5 — Second client takeover.** With a session running, attach from a
second terminal. Expected: the first one is told and exits cleanly; the
second shows the full screen. ☐

## G. Install / setup

**G1 — `termocode setup`.** Expected: it reports your terminal font and,
if it is not a Nerd Font, exact steps for your terminal. ☐

**G2 — Doctor.** Help → Doctor: the "terminal font" row is correct. ☐

## H. Terminals and look (2026-10-15)

**H1 — kitty keys** (kitty, Ghostty, WezTerm or foot). In a Go file press
`Ctrl+Shift+M` → the Problems panel; `Ctrl+Shift+O` → symbols in the
file; `Ctrl+.` → code actions; open two files, `Ctrl+Tab` switches between
them; in the AI input `Shift+Enter` makes a new line. `Ctrl+[` still
leaves insert mode. Quit, then in the shell `Ctrl+C` and arrow keys work
as before. ☐

**H2 — GNOME Terminal unchanged.** In GNOME Terminal all your usual keys
work exactly as before (the protocol stays off there). Quit: the
terminal's background colour comes back. ☐

**H3 — Scrollback kept** (kitty or Windows Terminal). Run a few commands,
start termocode, quit. Expected: the earlier commands are still in the
scrollback and the background colour is unchanged. ☐

**H4 — Alt+X palette.** Press `Alt+X` → the command palette opens. Also
inside the VS Code integrated terminal, where `F1` is taken by VS Code. ☐

**H5 — Copy toast** (xterm or macOS Terminal.app, no wl-copy / xclip).
Copy a word. Expected: a warning that says how to make copying work, not
"Copied (via terminal)". ☐

**H6 — Light themes.** Theme Catppuccin Latte, then Rosé Pine Dawn.
Expected: the breadcrumb folders, "AI off", the hints at the bottom of
dialogs and the version line on the start page are easy to read; on Dawn
the sidebar is not brighter than the editor. ☐

**H7 — Dialog shadow.** Open the palette (`F1`) and a toast (save a
file). Expected: a soft shadow, not a black bar; the page behind the
palette is dimmed but readable. ☐

**H8 — Hover and selection.** Move the mouse over the explorer, Source
Control and the activity bar; select rows with the keyboard in each.
Expected: hover, selection and focused selection look the same in every
panel. ☐

**H9 — Tilix / Terminator.** If you use one: icons are Unicode (no tofu
boxes) unless you set Nerd icons yourself. ☐

## S. Over SSH (2026-10-15)

Automated: e2e `TestSSHClipboardOSC52`, `TestSlowLink`,
`TestSSHClientLook`, `TestInsideMultiplexer`, `TestSSHSessionPrivateAndRSS`
(a fake terminal and a fake slow line). These steps check the real thing.

**S1a — Copy over SSH, kitty.** `ssh` from kitty into a server, run
termocode, select a word in the editor, copy (palette "Copy: File:Line
Reference" too). Paste on your **own** computer (gedit, browser).
Expected: the same text; toast "Copied (via terminal)". ☐

**S1b — Copy over SSH, GNOME Terminal.** Same in GNOME Terminal (it does
not take OSC 52). Expected: the toast does not say "Copied" for your own
clipboard; it says how to make copying work. ☐

**S1c — Copy inside tmux on the server.** In kitty: `ssh` → `tmux` →
termocode, copy, paste locally. Try `tmux set -g set-clipboard on` and,
on tmux 3.3+, `tmux set -g allow-passthrough on`; with either one the
text arrives. Note your tmux version. ☐

**S1d — Copy inside GNU screen on the server.** In kitty: `ssh` →
`screen` → termocode, copy a word and a long block (~3000 characters),
paste locally. Expected: both arrive whole. ☐

**S2 — Slow line.** On a real slow line (phone hotspot, or on a test
server as root: `tc qdisc add dev eth0 root netem delay 150ms rate
1mbit`; remove it after with `tc qdisc del dev eth0 root`). Hold ↓ for
3 s in a long file, let go; scroll with the wheel, stop; type fast.
Expected: everything stops within about half a second of letting go; no
`^[[0n` or other junk in the shell after quitting. ☐

**S3a — No Nerd Font on your computer.** From a terminal with a normal
font (GNOME Terminal, Ubuntu Mono) `ssh` and run termocode. Expected:
Unicode icons, no boxes with hex numbers. ☐

**S3b — No truecolor.** From a terminal that shows only 256 colours (or
Terminal.app before macOS 26) run `TERMOCODE_COLORS=256 termocode` on the
server. Expected: colours look right, no wrong bright blocks. Without the
variable, write down whether colours look right. ☐

**S3c — Mouse and Persian with no server locale.** On a server where
`locale` shows `LANG=` (or run `env -u LANG -u LC_ALL termocode`), open a
file with Persian text, click in it, scroll with the wheel. Expected:
Persian letters joined and right-to-left, not `<d8>` codes; mouse clicks
land on the right cell. ☐

**S4 — termocode inside tmux / screen on the server.** In tmux: arrows,
Home / End, Ctrl+←/→, Shift+Tab, `F1`, Alt+X, mouse click and wheel all
work; colours look right; `termocode sessions` lists no session (tmux
keeps it alive); quit: the pane has its normal colours and scrollback.
Repeat in `screen`. ☐

---

**When something fails:** note the case ID, what you did, what you saw
(a screenshot helps), and paste it to the AI/maintainer. Most cases map
to a test in `internal/testkit` or `e2e/` that should then be extended so
the bug can not come back.

## V. Services view with real Docker (2026-10-17)

Automated: `internal/services` (fake `docker`), testkit
`TestServices*`, `TestAIServicesDatabaseFlow`. These steps need a real
Docker and are never run by the tests.

**V1 — Pull progress.** A project with `compose.yaml` using an image you do
not have (`docker image rm redis:7` first). Services view → `s` on redis.
Expected: the **services: up** tab shows the pull progress bars; the row is
`◌` until it runs, then `◉`. ☐

**V2 — Permission denied.** As a user outside the `docker` group, open the
view. Expected: "No access to the Docker socket", the `sudo usermod` line,
`y` copies it, nothing runs `sudo`. ☐

**V3 — Daemon down.** `sudo systemctl stop docker docker.socket`, then `R`.
Expected: "Docker is not running", rows keep their names with `?`. Start
Docker, `R`: the rows come back. ☐

**V4 — Events.** With the view open, `docker compose stop redis` in another
terminal. Expected: the row turns `◦` within a second, without `R`. Hide
the view (Explorer): `pgrep -af "docker events"` shows nothing. ☐

**V5 — Exit toast.** Make a service that exits 1 (`command: sh -c "exit
1"`), start it. Expected: `✘ exit 1`, one toast "Service … exited with code
1". ☐

**V6 — Podman shim.** With `podman-docker` (a `docker` that is podman) and
`podman compose`: the list and start / stop work, or the view shows a clear
note. Note the podman version. ☐

**V7 — SSH copy hint.** Over SSH, `y` on postgres. Expected: toast shows
`ssh -L 5432:localhost:5432 <server ip>`. ☐

**V8 — Quit.** Start mailhog from the view, Ctrl+Q. Expected: the card
lists mailhog only; **Keep running and quit** leaves it running (`docker
compose ps`). ☐
