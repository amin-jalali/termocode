# QC — per-feature test cases

One small, focused QC file per **meaningful unit** of behaviour (not per broad
feature). Each file is a fill-in skeleton: one scenario, steps, expected
result, and a status line. Keep units tiny — if a file needs "and also…", split
it into another file.

Files are named `<PREFIX>-<NN>-<unit>.md` and grouped in one folder per area.
Copy [`TEMPLATE.md`](TEMPLATE.md) to start a new case.

## Automation policy
**Automate as much as possible; manual checks are the fallback.** When you fill
in a case, set its `Automated check`:

- `✅ <path>_test.go` — a Go test covers it now;
- `🤖 TODO: <unit|integration>` — it could be automated but is not yet;
- `👁 manual (<why>)` — needs a real terminal, real nvim, or a human eye
  (rendering, colours, mouse, terminal-specific keys).

## Status legend
`⬜ untested` · ✅ pass · ⚠️ partial · ❌ fail · — n/a

## Areas & prefixes

| Prefix | Area | Folder |
|---|---|---|
| `SR` | Workspace search | [search/](https://github.com/amin-jalali/termocode/tree/main/docs/qc/search) |
| `KM` | Keymap / settings | [keymap/](https://github.com/amin-jalali/termocode/tree/main/docs/qc/keymap) |
| `HD` | Help / doctor | [help/](https://github.com/amin-jalali/termocode/tree/main/docs/qc/help) |
| `EX` | Extensions | [extensions/](https://github.com/amin-jalali/termocode/tree/main/docs/qc/extensions) |

Add a row (and a folder) when you start cases for a new area, e.g. `ED` editor,
`GIT` Source Control, `RD` run/debug, `TM` terminal, `TH` themes, `SS` session.

## Cases

| ID | Title | Automated check | Result |
|---|---|---|---|
| [SR-01](search/SR-01-find-text-with-rg.md) | Find text with ripgrep | ✅ `TestRun_LiveRipgrep` (needs rg) | ⬜ |
| [SR-02](search/SR-02-fallback-without-rg.md) | Search works without ripgrep | ✅ `TestRunFallback` | ⬜ |
| [SR-03](search/SR-03-fallback-skips-dirs-and-binaries.md) | Fallback skips vendored dirs and binaries | 🤖 TODO: unit | ⬜ |
| [KM-01](keymap/KM-01-override-from-keymap-json.md) | Override loaded from `keymap.json` | 🤖 TODO: unit | ⬜ |
| [KM-02](keymap/KM-02-bad-keymap-json-ignored.md) | Corrupt `keymap.json` does not break startup | 🤖 TODO: unit | ⬜ |
| [HD-01](help/HD-01-run-doctor.md) | `Help: Run Doctor` shows the report | ✅ `TestDoctorReport` (report text) · 👁 overlay | ⬜ |
| [EX-01](extensions/EX-01-broken-extension-does-not-block.md) | A broken extension does not block startup | ✅ `TestExtensionHostNvim` · 👁 toast | ⬜ |

## Environment
Record the environment in each case you run: OS, terminal emulator, `nvim
--version` first line, and whether `rg` is installed. Example:
`Ubuntu 24.04 · ghostty 1.1 · NVIM v0.10.1 · rg 14.1`.
