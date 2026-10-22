# Import settings from another editor

Moving to termocode? You do not have to set everything up again.
**Preferences: Import from Another Editor...** reads the settings of an
editor you already use. It shows you every change. It writes only after you say yes.

## What it brings over

- **Settings** that termocode has: tab size, word wrap, auto save, and a few more.
- **Key bindings** for actions termocode has. They go into `keymap.json`.
- **Theme**: the same theme if termocode has it, else the closest one as a suggestion.
- **Extensions and plug-ins** as *suggestions* only: which language servers
  or debuggers you may want, and which built-in feature does the same job.

Everything else is listed in the review panel as *not imported*, with the
reason. Nothing is dropped in silence.

The full tables are in [Import mappings](../reference/import-mappings.md).

## Which editors it looks for

termocode looks in the usual folders. It only reads; it never creates them.

| Editor | Linux | macOS |
| --- | --- | --- |
| VS Code, VSCodium, Cursor, Windsurf | `~/.config/<Code, VSCodium, Cursor or Windsurf>/User` | `~/Library/Application Support/<name>/User` |
| Zed | `~/.config/zed` | `~/.config/zed` |
| Helix | `~/.config/helix` | `~/.config/helix` |
| JetBrains IDEs | `~/.config/JetBrains/<product><version>` | `~/Library/Application Support/JetBrains/<product><version>` |
| Sublime Text 4 and 3 | `~/.config/sublime-text/Packages/User` (Text 3: `sublime-text-3`) | `~/Library/Application Support/Sublime Text/Packages/User` (Text 3: `Sublime Text 3`) |
| Vim | `~/.vimrc` | `~/.vimrc` |

If `XDG_CONFIG_HOME` is set on Linux, it is used instead of `~/.config`.

An editor is listed when it has at least one of these files:

- **VS Code family:** `settings.json` or `keybindings.json`. Installed extensions are read too.
- **Zed:** `settings.json` or `keymap.json`.
- **Helix:** `config.toml` or `languages.toml`.
- **JetBrains:** an `options` or `keymaps` folder. If several versions of one
  product exist, the newest is used.
- **Sublime Text:** `Preferences.sublime-settings`, `Package Control.sublime-settings`
  or a user key map. Text 4 is preferred over Text 3.
- **Vim:** `.vimrc`. Vim gives no settings or keys: the row only reminds you
  that you can load your own vimrc.

## Step by step

1. Open the palette (`F1`) and run **Preferences: Import from Another Editor...**.
   You can also use the **Import** row on the first-run setup card. See
   [First 5 minutes](../getting-started/first-steps.md#first-run-the-setup-card).
2. **Pick an editor.** Each row says what termocode found, for example
   "settings, 3 keys, theme", and where.
3. **Look at the review panel.** The left side is what the other editor has.
   The right side is what termocode will set. If you already have a value, you
   see `old → new`.
4. **Choose rows.** Use these keys:

    | Key | What it does |
    | --- | --- |
    | `Space` or `x` | Turn the row on or off. On a section title: the whole section. |
    | `a` / `n` | Turn all rows on / off. |
    | `Up` / `Down` (or `k` / `j`) | Move. `PgUp`, `PgDn`, `Home`, `End` also work. |
    | `[` / `]` | Jump to the previous / next section. |
    | `Enter` | Go on to the final check. |
    | `Esc` or `q` | Close the panel. Nothing is written. |

5. **Confirm.** A last card lists what will be written and to which file. Pick
   **Import**, or **Back** to return to the panel.
6. **Done.** termocode saves a backup, writes the files, and applies the new
   keys, theme and settings at once. You do not need to restart.

## Undo

Run **Preferences: Undo Last Import**. termocode asks first, then puts
`config.json` and `keymap.json` back exactly as they were before the import. A
file the import created is removed. Undo works after a restart too.

!!! note "Undo goes back to the moment before the import"
    Changes you made to these two files after the import are undone as well.

The setup card shows **Imported from ...** after an import. Its *Undo...*
button does the same as the palette command.

## What it never does

- It **never installs** anything. Extensions only become suggestions. Add the
  tools yourself with **LSP: Manage Language Servers...**.
- It **never changes the other editor's files**. They are only read.
- It **never writes before the final confirm**.
- It writes **only** `config.json` and `keymap.json` in the termocode config folder.
- It never replaces your own work: a key you already use is not taken over
  unless you turn that row on, and the rest of your `keymap.json` is kept.

## Where the backup is

Before each import, termocode copies the files it will change to
`~/.config/termocode/backup/import-<date>-<time>/` (or under `$XDG_CONFIG_HOME/termocode/`).
The folder holds the old `config.json` and `keymap.json` and a small
`manifest.json`. The file `backup/last-import.json` tells Undo which backup to use.
Old backups stay until you delete them.

If a write fails half way, termocode puts your files back at once and tells you.

## Troubleshooting

**"No other editor found"**
:   termocode did not find a settings folder in the places above. Check that the
    editor has been opened at least once, and that the folder exists. If your
    editor keeps its settings elsewhere (a portable install, a custom
    profile), this import cannot see it yet.

**A row is dim and cannot be turned on**
:   The row says why. The common reasons:

    - termocode has no matching setting, command or tool.
    - The binding is a two-key chord, or uses a key termocode cannot take
      (for example a plain letter without `Ctrl` or `Alt`).
    - The value is already the same in termocode ("already set").
    - The theme has no built-in match. The closest theme is only a suggestion,
      so it starts off.

**A key row is on "conflict"**
:   Another action already uses that key, in the defaults or in your
    `keymap.json`, or two imported keys want the same key. The row starts off.
    Turn it on if you want the imported key to win.

**Cmd keys on macOS**
:   Terminals do not send the Cmd key. A `Cmd+` binding is turned into `Ctrl+`,
    and the row says so.

**"Could not read ..."**
:   The other editor's file is damaged, or not valid JSON, XML or TOML. Fix it in the
    other editor, or skip that editor. Nothing was changed.

**Import failed**
:   Your files are put back as they were. The message says which file failed.
    Check the folder rights and free disk space, then try again.

**Undo says "Nothing to undo"**
:   There is no import to undo, or it was already undone.
