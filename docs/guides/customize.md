# Customize termocode

Make termocode yours: theme, settings, keys, snippets and your own commands.
Everything is saved in `~/.config/termocode/` (or `$XDG_CONFIG_HOME/termocode/`).

!!! info "Config can never break startup"
    If a config file has a typo or bad JSON, termocode ignores the bad part
    and starts anyway. Check **Help: Show Error Log** if a change does not
    take effect.

## Pick a theme

1. Press `F1` and run **Preferences: Color Theme…**.
2. Move through the list: each theme is shown **live** as you move.
3. Press `Enter` to keep it, or `Esc` to go back to your old theme.

Built-in themes:

| Dark | Light |
|---|---|
| **VSCode Dark+** (default), **GitHub Dark**, **One Dark**, **Solarized Dark**, **Catppuccin Mocha**, **Tokyo Night**, **Rosé Pine** | **Catppuccin Latte**, **Rosé Pine Dawn** |

A theme colours everything: the chrome (tabs, sidebars, pickers, status
bar) **and** the editor (syntax, diff, git signs, diagnostics, AI ghost
text, debug and test markers, the integrated terminal's colours).

![Catppuccin Latte](../assets/screenshots/theme-catppuccin-latte.png)

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
| `font_delta` | `0` | Make the font smaller (negative) or bigger while termocode runs. Only kitty can do this, and only with `allow_remote_control yes` in `kitty.conf`. In other terminals, use the terminal's zoom (Ctrl+-). Restart to apply. |
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
    Any key name your terminal can send: optional `ctrl+`, `alt+`, `shift+`,
    then one character (`ctrl+k`, `alt+T`, `ctrl+/`) or a named key
    (`f1`–`f24`, `enter`, `tab`, `esc`, `space`, `up`, `pgdown`, …). Write
    them in lower case, like in [Keyboard shortcuts](../reference/keys.md).
    Entries with a typo (a bad key like `ctrl+monkey`, or an unknown action
    name) are skipped. termocode shows a toast at startup and writes the
    details to `errors.log`. Some keys never reach the app because the
    terminal keeps them (for example `ctrl+shift+t` in many terminals).

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

## Look and feel: icons, animations, images

Three settings (Settings → **Appearance**) control how rich the screen is.
Each has an `auto` value that picks the best your terminal can show.

### Icons — `icons`

| Value | What you get |
|---|---|
| `auto` (default) | `nerd` on terminals that bundle the Nerd symbols (WezTerm, Ghostty, kitty), `ascii` on a bare console (`TERM=linux`) or a non-UTF-8 locale, else `unicode`. An installed Nerd Font alone is not enough: set `icons` to `nerd` once your terminal uses it (restart the terminal after `termocode setup`). |
| `nerd` | File-type, folder, git and panel icons from a [Nerd Font](https://www.nerdfonts.com/) (`termocode setup` installs one on Linux) |
| `unicode` | Plain one-cell symbols (▪ ◫ ⌥ ✓ ⚙) that render in any modern terminal font |
| `ascii` | Letters only — the safest choice for very old terminals |

`TERMOCODE_ICON_MODE=nerd|unicode|ascii` and `TERMOCODE_NERD_FONT=1` win over
the setting.

### Animations — `animations`

Pickers, the palette and dialogs slide and fade in (about 150 ms), toasts
slide in and out, and long jumps in lists scroll smoothly. The editor text
itself never animates.

| Value | What you get |
|---|---|
| `auto` (default) | On, except over SSH (`SSH_CONNECTION` / `SSH_TTY`) or when the terminal is not truecolor |
| `on` / `off` | Always / never |

When nothing moves, no animation timer runs (no idle CPU).

### Images — `images`

Opening a PNG, JPEG, GIF or WebP file shows it in a preview card. The
dashboard logo and images in the Markdown preview (`F7`) use the same code.

| Value | What you get |
|---|---|
| `auto` (default) | Ask the terminal at start: kitty graphics, else sixel, else half-blocks |
| `kitty` | Real images with the kitty graphics protocol |
| `sixel` | Real images with sixel |
| `blocks` | Coloured half-block characters (`▀`) — works in every colour terminal |
| `off` | No images |

Inside **tmux**, real images need `set -g allow-passthrough on`; without it
termocode uses half-blocks. **Help: Run Doctor** shows what was detected.

### Colours

termocode sends 24-bit colour by default, because most terminals support
it even when they do not say so (and `COLORTERM` is often lost over SSH).
It uses fewer colours on its own for `TERM=linux`, `TERM=dumb` / `NO_COLOR`
(no colour), and when you set `TERMOCODE_COLORS=truecolor|256|16|none`.
On 16 colours the dimmed background behind dialogs uses the terminal's
"faint" style instead of colour blending.

### What works where

| Terminal | Colours | Images | Notes |
|---|---|---|---|
| kitty, Ghostty | truecolor | kitty | everything |
| WezTerm | truecolor | kitty (sixel too) | everything |
| iTerm2 | truecolor | sixel | |
| foot | truecolor | sixel | |
| Windows Terminal | truecolor | sixel (1.22+) | |
| GNOME Terminal / VTE, Konsole, Alacritty | truecolor | half-blocks | |
| VSCode terminal | truecolor | half-blocks | |
| tmux | as the outer terminal | half-blocks; real images with `allow-passthrough on` | |
| SSH session | as the client terminal | as the client terminal | animations off by default |
| Linux console (`TERM=linux`) | 16 colours | half-blocks | ASCII icons |

### Font size

| Environment variable | Effect |
|---|---|
| `TERMOCODE_FONT_DELTA=0` | Do not change the terminal font size |

## Keys at a glance

| Command | Action |
|---|---|
| **Preferences: Color Theme…** | Pick a theme (live preview) |
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
:   That is `font_delta` (kitty only). Set it to `0` in Settings, or start with
    `TERMOCODE_FONT_DELTA=0`. termocode puts the size back when it exits.

**`font_delta` does nothing**
:   Most terminals (iTerm2, WezTerm, Ghostty, GNOME Terminal, Windows
    Terminal, foot …) do not let programs change the font size. Use the
    terminal's zoom (Ctrl+- / Ctrl+=). In kitty, add `allow_remote_control yes`
    to `kitty.conf`. It does not work inside tmux.

**I broke `keymap.json`**
:   termocode ignores a broken file and uses the defaults. Fix the JSON or
    delete the file.

**Import did not change everything**
:   Some settings need a restart. Run **Developer: Reload Window**.
