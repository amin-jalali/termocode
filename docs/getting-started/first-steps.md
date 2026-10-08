# Your first 5 minutes

This page walks you through one short session. You open a project, find a
file, edit it, save it, commit it and run a command in the terminal.

!!! tip "One key to remember: `F1`"
    `F1` opens the **command palette**. Every action is in it. Each row also
    shows its shortcut, so the palette teaches you the keys.

## 1. Open a project

```sh
cd ~/code/my-project
termocode
```

You see the **welcome screen**. It has five cards: *Open file*,
*Command palette*, *Find in files*, *Open shell* and *Clone repository*.
Below them are your recent files and recent workspaces.

Use the arrow keys and `Enter`, or click a card.

## 2. Find a file with `Ctrl+P`

1. Press `Ctrl+P`.
2. Type a few letters of the file name. The list filters as you type.
3. Press `Enter` to open the file.

The file opens in a tab. Press `Ctrl+B` to show or hide the file explorer.
Press `F6` to move focus between the sidebar and the editor.

## 3. Run any command with `F1`

1. Press `F1`.
2. Type `theme`.
3. Pick **Preferences: Color Theme** and choose a theme.

Commands you use often move to the top of the list.

## 4. Edit — with or without Vim

The editor is a real Neovim. You can work in two ways:

- **Like VSCode.** Type to insert text. Use `Shift+arrows` to select,
  `Ctrl+C` / `Ctrl+V` to copy and paste, `Ctrl+Z` to undo.
- **Like Vim.** Press `Esc` to go to Normal mode. All Vim motions work:
  `dd`, `ciw`, `gg`, `.` and the rest.

A few IDE keys to try:

| Key | What it does |
|---|---|
| `Ctrl+/` | Comment or uncomment the line |
| `Ctrl+D` | Select the next match (multi-cursor) |
| `F12` | Go to definition (needs a language server) |
| `Ctrl+.` | Quick fix / code actions |

## 5. Save with `Ctrl+S`

Press `Ctrl+S`. The dot on the tab goes away. If a language server is
running, the file is also formatted on save.

## 6. Commit in the git panel

1. Click the **Source Control** icon in the activity bar (left edge).
   Or run **Git: Focus Source Control** from `F1`.
2. Move to your file and press `s` to stage it.
3. Press `c` to jump to the message box. Type a message.
4. Press `Enter` to commit.
5. Click **⟳ Sync** to pull and push.

See the [Git guide](../guides/git.md) for hunks, diffs and conflicts.

## 7. Open the terminal with `Ctrl+T`

Press `Ctrl+T`. A shell opens in the bottom panel. Run your build or tests
there. Press `Ctrl+T` again to hide it.

## 8. Quit

Press `Ctrl+Q`. Next time you open this folder, your tabs and cursor
positions come back.

## Keys at a glance

| Key | Action |
|---|---|
| `F1` | Command palette |
| `Ctrl+P` | Go to file |
| `Ctrl+B` | Toggle the file explorer |
| `F6` | Switch focus between sidebar and editor |
| `Ctrl+S` | Save |
| `Ctrl+W` | Close the tab |
| `Ctrl+F` | Find in the file |
| `F8` | Find in all files |
| `Ctrl+T` | Toggle the terminal |
| `Esc` | Vim Normal mode |
| `Ctrl+Q` | Quit |

Run **Help: Show Shortcuts** from the palette for the full cheat sheet, or see
[Keyboard shortcuts](../reference/keys.md).

## Troubleshooting

**A key does nothing**
:   Your terminal may catch it first. For example, many terminals use `F11`
    for full screen and `Ctrl+Shift+T` for a new tab. Use the palette (`F1`)
    instead — every action is there.

**`F12` or `Ctrl+.` does nothing**
:   You probably have no language server for this file type. See
    [Install language servers](../guides/language-servers.md).

**Something looks broken**
:   Run **Help: Run Doctor** from the palette. See [Run Doctor](help-doctor.md).
