# SR-01 — Find text with ripgrep

> Area: Workspace search · Unit: `search.Run (rg path)`

- **Priority:** High
- **Precondition:** `rg` on `$PATH`. termocode open on a folder that contains
  `a.go` with the line `func Hello() {}` and `b.go` with `// hello world`.
- **Test Data:** query `hello`, then `Hello`
- **Environment:** <OS · terminal · nvim version · rg version>
- **Traces to:** Find in Files · `internal/search/search.go`, `internal/search/model.go`
- **Automated check:** `✅ internal/search/search_test.go: TestRun_LiveRipgrep` (skips without rg)

**Scenario:** smart-case search returns every match with file and line.

## Steps
1. Press `F8` (or `Alt+F`).
2. Type `hello` and wait for the results.
3. Clear the input and type `Hello`.
4. Press `Enter` on the first result.

## Expected
- Step 2: two results — `a.go:1` and `b.go:1` (lower-case query ignores case).
- Step 3: one result — `a.go:1` (an upper-case letter makes it case-sensitive).
- The matched text is highlighted in each row.
- Step 4: `a.go` opens in the editor with the cursor on line 1.

## Actual
_(filled at run time)_

## Result
`⬜ untested`  ·  ✅ pass / ⚠️ partial / ❌ fail / — n/a
