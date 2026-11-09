# Workspaces and sessions

termocode remembers your work. It can also show several project folders in one
window.

## Two ideas that are easy to mix up

| | Add Folder (multi-root) | Open Recent Workspace |
|---|---|---|
| What it does | adds a folder to **this** window | switches to **another** project |
| Main folder | stays the same | becomes the new folder |
| Saved in | the workspace session (`sessions/`) | `workspaces.json` |

## Add a second folder to the window

1. Press `F1` and run **Workspace: Add Folder...**.
2. Type the folder path and press `Enter`. `~` works.
3. The folder shows as a new root in the explorer, below the main one.

Now these work across **every** folder:

- Go to file (`Ctrl+P`) — files from extra folders start with the folder name,
  e.g. `api/main.go`.
- Find in Files (`F8`), the live overlay and Replace in Workspace.

To remove a folder, run **Workspace: Remove Folder...**. The main folder (the one
you started termocode in) cannot be removed.

## Switch to another project

1. Run **File: Open Recent Workspace...**.
2. Pick a folder.

termocode saves your current session and restarts in that folder. You can
also click a workspace on the welcome screen.

## What termocode remembers

termocode saves your session when you work and restores it on the next start:

- open tabs and the cursor in each file,
- open folders in the explorer,
- extra workspace folders,
- undo history for each file,
- the Source Control tree / flat choice,
- the last AI chat,
- terminal tabs, their split panes and folders (new shells, no output: see
  [The terminal](terminal.md#after-a-restart)).

It also keeps your recent files (`Ctrl+R`), recent workspaces, breakpoints and
theme.

Each project has its **own** session. When you switch projects, you get the
tabs of that project, not the ones from your last run. termocode keys the
session by the folder you start it in (the main folder). The files live in
`~/.config/termocode/sessions/`, one per folder.

The terminal height, the Actions panel and the Source Control tree / flat
choice are the same in every project. They stay in
`~/.config/termocode/session.json`.

!!! note "Upgrading from an older termocode"
    Older versions kept one `session.json` for all projects. The first time
    you open the project it belongs to, termocode moves it into
    `sessions/`. Other projects start empty.

Over SSH the whole editor also survives a dropped connection: see
[Remote and SSH](remote-ssh.md).

## Layouts

The automatic restore above brings back what you had. A **layout** is a named
arrangement you choose on purpose: which panels are open and how big they are,
the editor splits, the terminal tabs and, if you like, the open files. Use it to
switch between ways of working, for example "debug" or "review", and to share a
setup with your team.

| Command | What it does |
|---|---|
| **Layout: Open...** (`Alt+Y`) | Pick a layout and open it. `Right` shows what it changes first. |
| **Layout: Save As...** | Save what is on screen under a name. |
| **Layout: Delete...** | Remove a saved layout. It asks first; Cancel is the default. |
| **Layout: Reset to Default** | Back to the first-start look and sizes. Open files stay. |

The status bar shows the layout name for 2 seconds after it opens.

**Built-in layouts** (they cannot be deleted; copy one with **Layout: Save As...**):

| Name | What it opens |
|---|---|
| `default` | Explorer and editor; chat and bottom panel closed |
| `focus` | The editor only |
| `debug` | Run and Debug view, a terminal, Debug Console and Problems |
| `review` | Source Control view and the AI chat |

**Where layouts live**

- For you, in every project: `~/.config/termocode/layouts/<name>.jsonc`
- For one project, to share: `.termocode/layouts/<name>.jsonc`. Commit it and
  your team gets it. When both have the same name, the project one wins.

**Save As** asks for a name, then "Only me" or "This project". Names may have
letters, digits, space, `-`, `_` and `.`. A layout file holds only the shape of
your window: no environment values, no secrets and no terminal output. File and
folder paths are relative to the project.

A layout file is plain JSON with comments. All parts are optional; a part you
leave out stays as it is when the layout opens:

```jsonc
{
  "description": "Editor, terminal and logs",
  "sidebar": { "open": true, "view": "git", "width": 40 },
  "panel": {
    "open": true, "rows": 14, "active": "terminal",
    "tabs": ["problems"],
    "terminals": [
      { "name": "api", "cwd": "cmd/api", "panes": [{ "split": "right", "cwd": "web" }] },
      { "name": "logs" }
    ]
  },
  "chat": { "open": false },
  "editor": { "split": "vertical", "panes": ["main.go", "main_test.go"], "files": ["README.md"] },
  "focus": "editor"
}
```

Sidebar views: `files`, `search`, `git`, `run`, `tests`, `containers`,
`databases`, `itermoc`, `runs`. Panel tabs: `terminal`, `problems`, `output`,
`tests`, `debug-console`.

A terminal tab can list its split `panes` (up to 3). Each pane has `split`
(`right` or `down`), `parent` (the pane it was split from: 0 is the tab's own
shell, 1 the first pane in the list) and `cwd`. Older layout files without
`panes` still work.

When a layout opens:

- Files that do not exist are skipped. One message says how many ("2 files not
  found").
- A panel that needs something you do not have (a database connection, a
  running debug session) opens with its normal empty state.
- Terminal tabs you already have stay; a layout only adds missing ones, each in
  its folder. A tab with a single shell gets the panes its entry lists.
- Zen mode ends, so the layout shows.

## Start the session at login

`termocode service install` makes the session of a folder come back
after a reboot (and so the workflows running in it).

```
cd ~/proj
termocode service install      # shows the file, asks [y/N], default no
termocode service status
termocode service uninstall
```

- Linux: a systemd user unit in `~/.config/systemd/user/`. To start it at
  boot even when you are not logged in, run once `loginctl enable-linger $USER`.
- macOS: a LaunchAgent in `~/Library/LaunchAgents/`. It starts at login.
- Only files in your home folder are written. No sudo.
- `uninstall` removes only a file that termocode wrote (it has a marker
  line). A file you made yourself is never touched or overwritten.
- `--yes` skips the question; `--dir d` picks another folder.
- Termux (Android) has no user services; the command says so.

## Start it from a script (no terminal)

`termocode serve [--web] [dir]` starts the session of a folder with no
terminal and returns at once. The UI runs in its own pty, the same way
as at login. Use it from the web panel or the phone. `--web` also starts
the web panel (it needs `"web_panel": "on"`).

```
termocode serve --web ~/proj   # start it; the web panel comes up too
termocode attach ~/proj        # see it in this terminal (detach to leave it)
termocode kill ~/proj          # stop it
```

## Reload into a new build

When you build termocode yourself, the running termocode sees the new
program file and moves into it by itself. You never restart it by hand.

- It checks its own program file every 3 seconds. Only a complete file
  counts: it must not change for one check, it must run, and
  `termocode --version` must answer. A half-written or broken file is
  ignored.
- It waits for a quiet moment: no AI turn, no agent at work, no command
  in a terminal tab, no open dialog or card. Until then the status bar shows **⬆ new build** and a
  toast says *New build ready — reloading when idle*.
- **termocode: Reload Into New Build** reloads now (on a page it asks
  first). **termocode: Reload New Build Later** keeps the build that runs.
  Both are in the laptop palette and in the page's palette.
- A reload works like **Developer: Reload Window**: the same process,
  terminal, folder and arguments. Unsaved text, the session and the web
  panel stay; programs in terminal tabs end, as on Reload Window. The
  web panel comes back on the same port, and each open page reconnects
  with its own sign-in: no new link, no new pairing. A page of the old
  build loads the new page by itself.
- The setting `reload_on_new_build` is on for dev builds (`dev`,
  `+dirty`, `v0.0.0-…`) and off for releases.

## Sessions in your menu bar

`termocode bar` prints a short summary of all running sessions and the
agents in their terminal tabs, for a menu bar, tray or status bar. The
title puts agents that wait for you first:

```
$ termocode bar
tc 1 waiting · 1 working
~/api  attached  claude waiting, codex working
~/web  detached  no agents
```

Pick the form your bar tool reads with `--format`:

| Form | For |
|---|---|
| `text` (default) | a quick look in a terminal, or any tool that shows the first line |
| `json` | status-bar modules: one line with `text`, `tooltip`, `class` (`none`, `idle`, `working`, `waiting`) and the `sessions` |
| `menu` | menu-bar tools that run a script plugin: the title, then per session its agents and an **Attach** item that opens the session in a terminal |

Run it every few seconds from the tool (a 5 s refresh is enough). Example
for a status-bar module that reads JSON:

```json
"custom/termocode": {
  "exec": "termocode bar --format json",
  "return-type": "json",
  "interval": 5
}
```

For a menu-bar tool with script plugins, save this as an executable
plugin file named like `termocode.5s.sh`:

```sh
#!/bin/sh
exec termocode bar --format menu
```

- It only reads. Nothing in the menu ends a session; use `termocode kill`
  in a terminal for that.
- Agent states come from the hooks (`termocode agent report`), else from
  the screen. A session that does not answer within 2 s shows "no answer".
- Only persistent sessions show. A termocode started with `--no-session`
  is not listed.

## Start fresh

- **Developer: Reset Session** clears the saved tabs and cursor history of
  this project. Other projects keep theirs. The old session is kept as a
  `.bak` file.
- **Developer: Reload Window** restarts termocode in the same folder and keeps
  the session. Use it after you change config or install a tool.

## Keys at a glance

| Key / command | Action |
|---|---|
| `Ctrl+R` | Recent files |
| `Alt+Y` | Open a saved layout |
| **File: Open Recent Workspace...** | Switch project |
| **Workspace: Add Folder...** | Add a root folder |
| **Workspace: Remove Folder...** | Remove a root folder |
| **Layout: Save As...** | Save the window layout under a name |
| **Developer: Reload Window** | Restart, keep session |
| **Developer: Reset Session** | Clear the session |

## Troubleshooting

**My extra folder is gone after a restart**
:   termocode drops folders that no longer exist on disk. Check the path.

**Tabs did not come back**
:   A file that was deleted is not reopened. Also check that you started
    termocode in the same folder: the session belongs to the main folder,
    so `termocode ~/proj` and `termocode ~/proj/sub` have different sessions.

**I want a totally clean start**
:   Run **Developer: Reset Session**.

## Preview and rename in the explorer

Focus the Files explorer and press `p`, or run **Explorer: Toggle Preview**.
Move up or down to preview a file beside the tree. Enter opens it in the editor.
Use `PgUp/PgDn` or the mouse wheel over the preview to scroll.
The preview shows up to 200 text lines and reads at most 64 KB. Images show their
size and dimensions. Other files show a binary notice; folders show file and
folder counts. Secret names such as `.env`, `.pem`, `.key` and `id_rsa` stay hidden.
Preview needs a terminal at least 100 columns wide.

Select files with `Ctrl+Space` or `Shift+Up/Down`. Press `r` or `F2`, choose
**Rename…** from the file menu, or run **Explorer: Rename Selected…**.
Edit one name per line. Use the arrow keys to move, Enter to add a line, and
`Ctrl+S` to review. The review lists changes and conflicts. Cancel has focus;
press Tab to reach Apply. Names must stay in the same folder. Swapping names
works. Open editor tabs follow the new names and keep unsaved text.
In a git repo, git follows too: a file with staged changes shows as
renamed in Source Control, and its staged text stays staged under the new
name. This also holds for one file or folder renamed with **Rename...**.

Click **Renamed N files · Undo** to reverse the rename, or run
**Explorer: Undo Rename**. Undo works while the renamed files are unchanged and
the old names are free. It covers the last bulk rename in this session.

## Move files by drag and drop

Drag a file or folder in the Files explorer and let go on a folder. Let go on a
file to move into that file's folder. The folder under the pointer is lit.
If the row you drag is part of a selection, the whole selection moves.
A card asks first. Cancel has focus; press Tab to reach Move, or click Move.
A move never replaces a file: a name that is taken stops it. A folder can not
move into itself, and a workspace root stays where it is. Let go outside the
explorer to drop nothing. Open tabs follow the move and keep unsaved text. In a
git repo, git follows too, as for a rename. Container workspaces move inside
the container.
