# Find and replace across files

termocode has four search tools. Pick the one that fits the job.

| You want to… | Use | Key |
|---|---|---|
| find in this file | the find bar | `Ctrl+F` |
| replace in this file | the replace bar | `Ctrl+H` |
| list every match in the project | the **Find Results** buffer | `F8` or `Alt+F` |
| jump to one match fast | the **live overlay** | palette: **Search: Find in Files (Live Overlay)** |
| replace in every file | Replace in Workspace | `Ctrl+Shift+H` |

All workspace search uses `rg` (ripgrep) when it is installed. Without it,
a built-in search runs instead. It is slower but gives the same results.

## Find in this file

1. Press `Ctrl+F`. If you selected text, it is already in the box.
2. Type. Every match is highlighted. The box shows `3/17`-style counts.
3. Press `Enter` for the next match, `Shift+Tab` for the previous one.
4. Press `Esc` to close.

Click the toggles on the right:

- `Aa` — match case
- `ab` — whole word
- `.*` — regular expression

## Replace in this file

1. Press `Ctrl+H`.
2. Type the search text, press `Tab`, type the replacement.
3. Press `Enter` to replace one match. Press `Ctrl+Enter` or `Alt+Enter`
   to replace all.

This is normal Neovim editing, so `Ctrl+Z` undoes it.

## Find in all files (Find Results buffer)

1. Press `F8` (or `Alt+F`).
2. Type your text and press `Enter`.
3. A **Find Results** tab opens. Hits are grouped by file.
4. Move to a hit and press `Enter`, or click it, to open that line.

Results stream in while the search runs. The tab stays open, so you can
come back to it.

The search uses *smart case*: all-lowercase text matches any case; text with a
capital letter matches exactly. When `rg` is installed, the text is a regular
expression (without `rg` it is plain text). So escape
characters like `.` or `(` if you mean them literally — or use the live
overlay, where regex is a toggle. It searches every folder of a
[multi-root workspace](workspaces.md).

## Live overlay

1. Press `F1` and run **Search: Find in Files (Live Overlay)**.
2. Type. Results update as you type.
3. Use `↑` / `↓` and `Enter` to open a hit. `Esc` closes the overlay.

The overlay has more options:

| Option | How |
|---|---|
| Match case | `Alt+C` or click `Aa` |
| Whole word | `Alt+W` or click `ab` |
| Regex | `Alt+R` or click `.*` |
| Include / exclude globs | `Tab` to the filter row, e.g. `*.go` or `vendor/**` |
| Context lines | `Tab` to the `context` field and type a number |

## Replace in every file

1. Press `Ctrl+Shift+H` (or run **Search: Replace in Workspace...**).
2. Type the text to find. Press `Enter`.
3. Type the replacement. Press `Enter`.
4. A dialog says how many matches and files will change, for example
   *Replace 12 matches in 3 files?* Pick **Replace** or cancel.

!!! warning "There is no undo across files"
    Replace in Workspace writes straight to disk. The search is literal
    and case-sensitive. Commit or stash your work first, so git can undo it.

## Result limit

Find in Files stops after **2000** results by default. The footer then says
*Showing N of M*. Change the limit with the `search_max_results` setting
(`0` means no limit). See [Settings](../reference/settings.md).

## Keys at a glance

| Key | Action |
|---|---|
| `Ctrl+F` | Find in file |
| `Ctrl+H` | Replace in file |
| `Enter` / `Shift+Tab` | Next / previous match (find bar) |
| `F8` or `Alt+F` | Find in all files |
| `Ctrl+Shift+H` | Replace in all files |
| `Alt+C` / `Alt+W` / `Alt+R` | Case / word / regex (live overlay) |

Want the full story of how search works? Read
[Find in files, in depth](../concepts/find-in-files.md).

## Troubleshooting

**`Alt+F` opens my terminal's menu**
:   Some terminals (GNOME Terminal) use `Alt+F` for the File menu. Use `F8`.

**`Ctrl+Shift+H` does nothing**
:   Your terminal may not send it. Run **Search: Replace in Workspace...**
    from the palette.

**Search is slow**
:   Install ripgrep (`rg`). Without it, the built-in search walks every file.

**Some files are missing from results**
:   With ripgrep, files in `.gitignore` are skipped. The built-in search skips
    `.git`, `node_modules`, `vendor`, build folders, dot-folders, binary files
    and files over 5 MiB.
