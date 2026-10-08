# Workspaces and sessions

termocode remembers your work. It can also show several project folders in one
window.

## Two ideas that are easy to mix up

| | Add Folder (multi-root) | Open Recent Workspace |
|---|---|---|
| What it does | adds a folder to **this** window | switches to **another** project |
| Like | VSCode "Add Folder to Workspace" | `cd` + restart |
| Main folder | stays the same | becomes the new folder |
| Saved in | `session.json` | `workspaces.json` |

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

!!! note "One session for all projects"
    Today termocode keeps **one** session file
    (`~/.config/termocode/session.json`), not one per project. If you switch
    between projects, the tabs you see are the ones from your last run.

## Start fresh

- **Developer: Reset Session** clears the saved tabs and cursor history.
  The old session is kept as a `.bak` file.
- **Developer: Reload Window** restarts termocode in the same folder and keeps
  the session. Use it after you change config or install a tool.

## Keys at a glance

| Key / command | Action |
|---|---|
| `Ctrl+R` | Recent files |
| **File: Open Recent Workspace...** | Switch project |
| **Workspace: Add Folder...** | Add a root folder |
| **Workspace: Remove Folder...** | Remove a root folder |
| **Developer: Reload Window** | Restart, keep session |
| **Developer: Reset Session** | Clear the session |

## Troubleshooting

**My extra folder is gone after a restart**
:   termocode drops folders that no longer exist on disk. Check the path.

**Tabs did not come back**
:   A file that was deleted is not reopened. The session is shared by all
    projects, so a run in another folder replaces the saved tabs.

**I want a totally clean start**
:   Run **Developer: Reset Session**.
