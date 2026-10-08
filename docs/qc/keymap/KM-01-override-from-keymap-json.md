# KM-01 — Override loaded from `keymap.json`

> Area: Keymap · Unit: `keymap.LoadOverrides` + `KeyMap.MergeInto`

- **Priority:** High
- **Precondition:** termocode closed. No `~/.config/termocode/keymap.json` yet.
- **Test Data:** `~/.config/termocode/keymap.json` with
  `{"ctrl+s": "SaveAll"}`; two open, modified files.
- **Environment:** <OS · terminal · nvim version>
- **Traces to:** Keymap · `internal/keymap/overrides.go`, `internal/app/model.go`
- **Automated check:** `🤖 TODO: unit` (LoadOverrides on a temp file → Match returns ActionSaveAll)

**Scenario:** a hand-written override changes what a key does after restart.

## Steps
1. Write the test data file.
2. Start termocode, open two files and change both.
3. Press `Ctrl+S`.

## Expected
- Both files are saved (both tabs lose the modified dot), not only the active one.

## Actual
_(filled at run time)_

## Result
`⬜ untested`  ·  ✅ pass / ⚠️ partial / ❌ fail / — n/a
