# KM-02 — Corrupt `keymap.json` does not break startup

> Area: Keymap · Unit: `keymap.LoadOverrides`

- **Priority:** Critical
- **Precondition:** termocode closed.
- **Test Data:** three variants of `~/.config/termocode/keymap.json`:
  1. `{not json`
  2. `{"ctrl+s": "NoSuchAction"}`
  3. `{"ctrl+monkey": "Save"}` (malformed key)
- **Environment:** <OS · terminal · nvim version>
- **Traces to:** [ADR 0005](../../adr/0005-config-never-breaks-startup.md) · `internal/keymap/overrides.go`
- **Automated check:** `✅ internal/keymap/overrides_test.go` (`TestLoadOverridesReport`)

**Scenario:** a bad override file is ignored and the defaults stay active.

## Steps
1. For each variant: write the file, start termocode, open a file, edit it,
   press `Ctrl+S`.

## Expected
- termocode starts normally every time (no crash, no blank screen).
- `Ctrl+S` saves the active file (default binding) in every variant.
- Variants 2 and 3 show a "keymap.json: 1 entry ignored" toast naming the entry.

## Actual
_(filled at run time)_

## Result
`⬜ untested`  ·  ✅ pass / ⚠️ partial / ❌ fail / — n/a
