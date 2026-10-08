# KM-02 — Corrupt `keymap.json` does not break startup

> Area: Keymap · Unit: `keymap.LoadOverrides`

- **Priority:** Critical
- **Precondition:** termocode closed.
- **Test Data:** three variants of `~/.config/termocode/keymap.json`:
  1. `{not json`
  2. `{"ctrl+s": "NoSuchAction"}`
  3. `{"ctrl+alt+shift+f19": "Save"}` (key the default keymap does not use)
- **Environment:** <OS · terminal · nvim version>
- **Traces to:** [ADR 0005](../../adr/0005-config-never-breaks-startup.md) · `internal/keymap/overrides.go`
- **Automated check:** `🤖 TODO: unit` (each variant → LoadOverrides returns empty map)

**Scenario:** a bad override file is ignored and the defaults stay active.

## Steps
1. For each variant: write the file, start termocode, open a file, edit it,
   press `Ctrl+S`.

## Expected
- termocode starts normally every time (no crash, no blank screen).
- `Ctrl+S` saves the active file (default binding) in every variant.

## Actual
_(filled at run time)_

## Result
`⬜ untested`  ·  ✅ pass / ⚠️ partial / ❌ fail / — n/a
