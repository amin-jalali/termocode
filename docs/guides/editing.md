# Edit and move around

This guide covers daily editing: files, tabs, code navigation, the terminal
and small power moves.

!!! info "The editor is real Neovim"
    Press `Esc` to drop into Normal mode at any time. Your Vim motions,
    registers, macros and `init.lua` all work. The IDE keys below are extra.

## Open files

1. Press `Ctrl+P` and type part of a name. Press `Enter`.
2. Or press `Ctrl+R` to pick from your **recent files**.
3. Or use the explorer: `Ctrl+B` shows it, `F6` moves focus into it.

In the explorer, right-click a file or folder for **New File**, **New Folder**,
**Rename**, **Delete** and more. `Shift+Click` and `Ctrl+Click` select many
files.

termocode opens text files only. Binary files, images and archives are
refused with a short message.

## Tabs and splits

| Key | Action |
|---|---|
| `Ctrl+PgDn` / `Ctrl+PgUp` | Next / previous tab |
| `Ctrl+Tab` | Switch to the last file |
| `Ctrl+W` | Close the tab |
| `Alt+Shift+T` or `Shift+F4` | Reopen the last closed tab |
| `Alt+P` | Pin or unpin the tab (pinned tabs stay at the front) |
| `Ctrl+\` | Split the editor to the right |

**Editor: Split Down** and **Editor: Close Split** are in the palette.

## Edit text

| Key | Action |
|---|---|
| `Ctrl+C` / `Ctrl+X` / `Ctrl+V` | Copy / cut / paste (system clipboard) |
| `Ctrl+Shift+V` | Clipboard history (last 20 copies) |
| `Ctrl+Z` / `Ctrl+Y` | Undo / redo |
| `Shift+arrows` | Select |
| `Ctrl+←` / `Ctrl+→` | Jump by word |
| `Ctrl+/` | Toggle line comment |
| `Alt+↑` / `Alt+↓` | Move the line |
| `Shift+Alt+↑` / `Shift+Alt+↓` | Duplicate the line |
| `Shift+Alt+F` | Format the document |
| `Alt+W` | Toggle word wrap |

The palette has more line tools: sort, reverse, UPPERCASE, lowercase,
join lines, trim trailing whitespace, collapse blank lines, and tabs ↔ spaces.

Copies also go through OSC 52. So copy works over SSH, if your terminal
supports it.

### Multi-cursor

| Key | Action |
|---|---|
| `Ctrl+D` | Select the word, then the next match |
| `Ctrl+Alt+↑` / `Ctrl+Alt+↓` | Add a cursor above / below |
| `Esc` | Leave multi-cursor mode |

### Snippets

Type a trigger word and press `Tab`. Then `Tab` and `Shift+Tab` jump between
the placeholders. Forgot the trigger? Run **Snippets: Browse...**.

Built-in triggers include `iferr`, `fori`, `test` (Go), `defm`, `cls`
(Python), `fn`, `afn` (JS/TS) and more. To add your own, see
[Customize › Snippets](customize.md#your-own-snippets).

## Code intelligence (LSP)

These keys need a language server for the file type. See
[Install language servers](language-servers.md).

| Key | Action |
|---|---|
| `F12` | Go to definition |
| `Ctrl+F12` | Go to type definition |
| `Shift+F12` | Find all references |
| `K` (Normal mode) | Hover documentation |
| `F2` | Rename symbol |
| `Ctrl+.` or `Alt+Enter` | Quick fix / code actions |
| `Ctrl+Shift+.` | Apply the preferred quick fix directly |
| `Ctrl+Shift+O` | Go to symbol in file |
| `Ctrl+Space` | Show completions |
| `F4` | Toggle inlay hints |
| `F5` / `Shift+F5` | Next / previous problem in the file |

**LSP: Go to Implementation** and **Go to Symbol in Workspace...** are in the
palette. Format on save is on by default.

## Bookmarks and jumps

| Key | Action |
|---|---|
| `Ctrl+G` | Go to line |
| `F3` | Toggle a bookmark on this line |
| `Shift+F3` | List all bookmarks |
| `Ctrl+Shift+E` | Reveal the current file in the explorer |

Bookmarks are Neovim global marks (`A`–`Z`), so `'A` works too.

## Terminal

| Key | Action |
|---|---|
| `Ctrl+T` | Show or hide the terminal panel |
| ``Ctrl+Shift+` `` | New terminal tab |
| `Ctrl+Shift+W` | Close the terminal tab |
| `Ctrl+Shift+PgDn` / `Ctrl+Shift+PgUp` | Next / previous panel tab |

The bottom panel also holds **Problems**, **Output**, **Test Results** and the
**Debug Console**. Run **View: Output** to see the Output tab.

Click a `file:line` link printed in a terminal tab to open that spot.

## Markdown, Zen and more

| Key | Action |
|---|---|
| `F7` | Markdown preview (`.md` files) |
| `Alt+Z` | Zen mode — hide everything except the editor |
| `Ctrl+Alt+I` | Buffer info |

## Keys at a glance

| Key | Action |
|---|---|
| `Ctrl+P` | Go to file |
| `Ctrl+R` | Recent files |
| `Ctrl+W` | Close tab |
| `Ctrl+/` | Toggle comment |
| `Ctrl+D` | Multi-cursor next match |
| `F12` | Go to definition |
| `F2` | Rename |
| `Ctrl+.` | Quick fix |
| `Ctrl+T` | Terminal |
| `Alt+Z` | Zen mode |

## Troubleshooting

**`Ctrl+Shift+...` keys do nothing**
:   Many terminals do not send `Ctrl+Shift` keys to apps. Terminals with the
    kitty keyboard protocol (kitty, WezTerm, Ghostty) work best. The palette
    always works.

**`F11` / `Shift+F11` make my window full screen**
:   Your terminal uses `F11`. Use the palette commands **Debug: Step Into** and
    **Debug: Step Out**, or change the terminal's setting.

**Copy does not reach my laptop over SSH**
:   Your terminal must allow OSC 52 clipboard writes. Enable it in the terminal
    settings. Very large copies (over about 6 KB) are skipped.
