# SR-03 — Fallback skips vendored dirs and binaries

> Area: Workspace search · Unit: `search.skipSearchDir`, `search.isBinary`

- **Priority:** Medium
- **Precondition:** `rg` not on `$PATH` (see [SR-02](SR-02-fallback-without-rg.md)).
  Workspace contains:
  - `src/main.go` with the text `needle`
  - `node_modules/x/index.js` with `needle`
  - `.cache/tmp.txt` with `needle`
  - `bin/blob` — a file with a NUL byte and the text `needle`
- **Test Data:** query `needle`
- **Environment:** <OS · terminal · nvim version · rg: no>
- **Traces to:** [ADR 0004](../../adr/0004-pure-go-search-fallback.md) · `internal/search/search.go`
- **Automated check:** `🤖 TODO: unit` (table test over a temp dir)

**Scenario:** the fallback walker ignores dependency/dot folders and binary files.

## Steps
1. Press `F8` and type `needle`.

## Expected
- Exactly one result: `src/main.go`.
- No rows from `node_modules/`, `.cache/` or `bin/blob`.

## Actual
_(filled at run time)_

## Result
`⬜ untested`  ·  ✅ pass / ⚠️ partial / ❌ fail / — n/a
