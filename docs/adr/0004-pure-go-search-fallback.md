# ADR 0004 — Pure-Go search fallback when ripgrep is missing

Status: **Accepted** (2026-10-08)

## Context
Find in Files (`F8` / `Alt+F`) used `rg --json` only. Without ripgrep on `$PATH`,
search just showed an error, which is a bad first experience — `rg` is common
for developers but not installed by default on most systems. Options:

1. Keep `rg` as a hard requirement and only improve the error message.
2. Shell out to `grep -rn` as a second tool.
3. Add a small search written in Go, used only when `rg` is missing.

`grep` flags and output differ between GNU and BSD, so option 2 adds parsing
work for each platform.

## Decision
`search.Run` / `search.RunDirs` (`internal/search/search.go`) check
`search.Available()`. If `rg` is missing they call `runFallback`, a
`filepath.WalkDir` walker that returns the same `Result` shape as the rg path:

- literal text, smart-case (any upper-case letter → case-sensitive);
- skips `.git`, `node_modules`, `vendor`, `target`, `dist`, `build`, `.next`,
  `.cache`, `__pycache__`, `.venv`, `venv` and every dot-folder;
- skips binary files (a NUL byte in the first 8 KiB) and files over 5 MiB;
- same caps as rg: `MaxResults` (100) and `MaxColumns` (200).

## Consequences
- **+** Workspace search works out of the box with no extra install.
- **+** One `Result` type, so the sidebar and pickers do not know which engine ran.
- **−** The fallback is simpler: no regex, no `.gitignore`, slower on big repos.
  ripgrep stays the recommended engine and `Help: Run Doctor` lists it.
- **−** Two code paths to keep in sync; `TestRunFallback*` in
  `internal/search/search_test.go` covers the fallback, and QC cases
  [SR-02](../qc/search/SR-02-fallback-without-rg.md) /
  [SR-03](../qc/search/SR-03-fallback-skips-dirs-and-binaries.md) cover it by hand.
