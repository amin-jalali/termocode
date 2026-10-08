# ADR 0005 — Config files can never break startup

Status: **Accepted** (2026-05-13, backfilled)

## Context
termocode keeps several JSON files in `~/.config/termocode/` (`config.json`,
`session.json`, `keymap.json`, `commands.json`, `user_theme.json`, …). Users
edit some of them by hand, and older versions of termocode may have written a
shape the current version no longer expects. A strict loader that refuses to
start on a bad file would lock the user out of the tool they need to fix it.

## Decision
Every config loader treats "missing", "unreadable", "corrupt JSON" and "unknown
values" as "use defaults" and never returns an error to the caller. For example
`keymap.LoadOverrides` (`internal/keymap/overrides.go`) returns `nil` on any
read/parse failure and drops entries with an unknown action name or a key the
default keymap does not use. Writers validate **before** they persist
(`keymap.IsKnownKey` in `applyKeybinding`), so the app itself never writes a bad
file. Real problems are surfaced as toasts and in `errors.log`, not as a failed
launch.

## Consequences
- **+** A broken or stale config file can always be fixed from inside termocode.
- **+** Old files keep working after upgrades; unknown fields are ignored.
- **−** Mistakes in hand-edited files fail silently (e.g. a typo in an action
  name in `keymap.json` is just ignored). The docs must say which values are
  valid, and a future "validate config" command would help.
