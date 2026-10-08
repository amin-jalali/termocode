# Customize termocode

Make termocode yours: theme, settings, keys, snippets and your own commands.
Everything is saved in `~/.config/termocode/` (or `$XDG_CONFIG_HOME/termocode/`).

!!! info "Config can never break startup"
    If a config file has a typo or bad JSON, termocode ignores the bad part
    and starts anyway. Check **Help: Show Error Log** if a change does not
    take effect.

## Pick a theme

1. Press `F1` and run **Preferences: Color Theme**.
2. Pick a theme and press `Enter`. It applies at once, with no restart.

Built-in themes: **VSCode Dark+** (default), **GitHub Dark**, **One Dark**,
**Solarized Dark**.

### Make your own colors

1. Run **Preferences: Edit Custom Theme**.
2. Pick a color token, for example a background, a syntax color or a git color.
3. Type a hex color like `#1e1e2e` and press `Enter`. It applies at once.

Your colors are saved in `user_theme.json`, on top of VSCode Dark+. A
**Custom** entry then appears in the theme list. To edit the file by hand, run
**Open Config: Theme**.

## Change settings

Run **Preferences: Open Settings (UI)**. You can search the list. Changes
apply right away.

| Setting | Default | What it does |
|---|---|---|
| `theme` | `vscode-dark-plus` | Color theme |
| `font_delta` | `-2` | Ask the terminal to make the font smaller (negative) or bigger. Restart to apply. |
| `auto_save` | `false` | Save changed files after about one second |
| `word_wrap` | `false` | Wrap long lines |
| `show_hidden` | `false` | Show dotfiles in the explorer |
| `tab_size` | `4` | Tab width (1–16) |
| `search_max_results` | `2000` | Find in Files limit (`0` = no limit) |

The AI settings are in the same list. See [Set up AI](ai-setup.md). For every
setting, see [Settings reference](../reference/settings.md).

!!! tip "Your Neovim config still applies"
    termocode runs your normal Neovim. Your `init.lua` options and plugins load
    too. EditorConfig files are honoured; **View: Show Buffer Info**
    (`Ctrl+Alt+I`) shows what applied.

## Change a key binding

1. Run **Preferences: Customize Keybindings**.
2. Each action shows its current key. Pick one and press `Enter`.
3. Type the new key, for example `alt+enter` or `f5`, and press `Enter`.
4. If the key is already used by another action, termocode warns you first.

Your changes are saved in `keymap.json`. It is a simple map from key to action
name:

```json
{
  "f5": "RunTask",
  "alt+t": "ShowProblems"
}
```

!!! note "Which keys can I use?"
    termocode only accepts key names it already knows — the keys that some
    default binding uses (`ctrl+…`, `alt+…`, `f1`–`f12`, and so on). A key that
    no default uses, like `ctrl+k`, is rejected. See
    [Keyboard shortcuts](../reference/keys.md) for the list.

Extensions can be bound too, with `"ext:<command-id>"` as the action (for
example `{ "alt+h": "ext:hello.say" }`). Extension bindings accept any key
name. See
[Write an extension](../extensions.md).

## Your own snippets

1. Run **Snippets: Manage...**.
2. Pick **+ New Snippet...** and type `scope:trigger`, for example
   `go:errw`. Use `*` as the scope for every file type.
3. Type the snippet body. Use `${1:name}` for placeholders and `$0` for the
   final cursor.

In the manager you can also edit or delete a snippet, filter by scope, or pick
**Open snippets.json** to edit the file:

```json
{
  "*":  { "todo": "// TODO(${1:me}): $0" },
  "go": { "errw": ["if err != nil {", "\treturn fmt.Errorf(\"${1:ctx}: %w\", err)", "}"] }
}
```

Your snippets win over the built-in ones when the trigger is the same.

## Your own commands

Put shell commands in `commands.json`. Run **Open Config: User Commands** to
open it.

```json
[
  { "id": "build",  "title": "Build",  "cmd": "go build ./..." },
  { "id": "deploy", "title": "Deploy", "cmd": "./scripts/deploy.sh staging" }
]
```

Each entry shows in the palette as **User: Build**, and in the task list
(`Alt+R`). `cmd` runs with `sh -c`, so pipes and `&&` work. For more power,
use [tasks](tasks-problems.md).

## Move your settings to another machine

1. Run **Preferences: Export Settings...** and choose a file path.
2. Copy the file to the other machine.
3. Run **Preferences: Import Settings...** there and choose the file.
4. Pick **Import & Reload** to apply everything. The other choice applies
   keys, theme and snippets now, and the rest after **Developer: Reload Window**.

The export holds your config files (settings, theme, keys, snippets,
commands…). It leaves out session state and **secrets**: API keys, tokens
and passwords are removed.

## Icons and font

| Environment variable | Effect |
|---|---|
| `TERMOCODE_ICON_MODE=nerd_font` | Use Nerd Font icons |
| `TERMOCODE_ICON_MODE=unicode` | Use plain Unicode icons |
| `TERMOCODE_ICON_MODE=ascii` | Safest; for terminals that draw icons badly |
| `TERMOCODE_FONT_DELTA=0` | Do not change the terminal font size |

## Keys at a glance

| Command | Action |
|---|---|
| **Preferences: Color Theme** | Pick a theme |
| **Preferences: Edit Custom Theme** | Edit colors |
| **Preferences: Open Settings (UI)** | Settings |
| **Preferences: Customize Keybindings** | Change keys |
| **Snippets: Manage...** | Your snippets |
| **Preferences: Export Settings...** / **Import Settings...** | Move settings |
| **Open Config: Theme** / **User Commands** / **AI Credentials** | Edit files by hand |

## Troubleshooting

**My key binding does not work**
:   The terminal may catch the key before termocode sees it. Try another key.
    Also check that the key name is one termocode knows (see the note above).

**My terminal font changed size when termocode started**
:   That is `font_delta` (default `-2`). Set it to `0` in Settings, or start with
    `TERMOCODE_FONT_DELTA=0`.

**I broke `keymap.json`**
:   termocode ignores a broken file and uses the defaults. Fix the JSON or
    delete the file.

**Import did not change everything**
:   Some settings need a restart. Run **Developer: Reload Window**.
