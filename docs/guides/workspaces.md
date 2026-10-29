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
- the last AI chat.

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
    "terminals": [{ "name": "api", "cwd": "cmd/api" }, { "name": "web", "cwd": "web" }]
  },
  "chat": { "open": false },
  "editor": { "split": "vertical", "panes": ["main.go", "main_test.go"], "files": ["README.md"] },
  "focus": "editor"
}
```

Sidebar views: `files`, `search`, `git`, `run`, `tests`, `containers`,
`databases`, `itermoc`, `runs`. Panel tabs: `terminal`, `problems`, `output`,
`tests`, `debug-console`.

When a layout opens:

- Files that do not exist are skipped. One message says how many ("2 files not
  found").
- A panel that needs something you do not have (a database connection, a
  running debug session) opens with its normal empty state.
- Terminal tabs you already have stay; a layout only adds missing ones, each in
  its folder.
- Zen mode ends, so the layout shows.

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
