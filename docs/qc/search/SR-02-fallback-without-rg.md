# SR-02 — Search works without ripgrep

> Area: Workspace search · Unit: `search.runFallback`

- **Priority:** High
- **Precondition:** `rg` is **not** on `$PATH` (e.g. start with
  `PATH=/usr/bin:/bin termocode` on a machine where rg lives elsewhere, or
  rename the binary). Same workspace as [SR-01](SR-01-find-text-with-rg.md).
- **Test Data:** query `hello`
- **Environment:** <OS · terminal · nvim version · rg: no>
- **Traces to:** [ADR 0004](../../adr/0004-pure-go-search-fallback.md) · `internal/search/search.go`
- **Automated check:** `✅ internal/search/search_test.go: TestRunFallback, TestRunFallback_SmartCase`

**Scenario:** with no ripgrep, Find in Files still returns results instead of an error.

## Steps
1. Run `Help: Run Doctor` and confirm the ripgrep row shows `?`.
2. Press `F8` and type `hello`.

## Expected
- No "ripgrep not found" error is shown.
- Two results — `a.go:1` and `b.go:1` — same as with rg.

## Actual
_(filled at run time)_

## Result
`⬜ untested`  ·  ✅ pass / ⚠️ partial / ❌ fail / — n/a
