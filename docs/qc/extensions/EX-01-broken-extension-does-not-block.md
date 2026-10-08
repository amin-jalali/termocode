# EX-01 — A broken extension does not block startup

> Area: Extensions · Unit: `internal/app/ext.go`, `internal/app/ext_lua.go`

- **Priority:** Critical
- **Precondition:** `~/.config/termocode/extensions/` holds `word-count`
  (Extensions: Install Sample Extensions) plus a folder `bad/` whose
  `init.lua` is `while true do end`, and a folder `typo/` whose `init.lua`
  is `this is not lua (`.
- **Test Data:** —
- **Environment:** <OS · terminal · nvim version>
- **Traces to:** [ADR 0006](../../adr/0006-lua-extension-host.md) · `internal/app/ext.go`
- **Automated check:** `✅ internal/app/ext_nvim_test.go: TestExtensionHostNvim` (Lua side) · 👁 toast / status bar

**Scenario:** termocode starts and stays usable while two extensions fail.

## Steps
1. Start `termocode` in a project folder.
2. Type in the editor right away.
3. Open the palette → **Extensions: Show Log**.

## Expected
- The editor accepts input at once; the UI never freezes.
- Within ~3 s an error toast names `bad` (timed out) and `typo` (syntax error).
- The status bar shows the word count (`word-count` still loaded).
- The log lists `bad ✘`, `typo ✘`, `word-count ✓`.

## Actual
_(filled at run time)_

## Result
`⬜ untested`
