# HD-01 — `Help: Run Doctor` shows the report

> Area: Help · Unit: `setup.Doctor` / `setup.Report` / `runDoctorCmd`

- **Priority:** Medium
- **Precondition:** termocode open on any folder. `nvim` and `git` installed.
- **Test Data:** —
- **Environment:** <OS · terminal · nvim version · rg yes/no>
- **Traces to:** Help: Run Doctor · `internal/setup/doctor.go`, `internal/app/doctor.go`
- **Automated check:** `✅ internal/setup/doctor_test.go: TestDoctorReport, TestDoctorMissingNvim` (report text) · `👁 manual` (overlay rendering)

**Scenario:** the palette command runs the read-only setup checks and shows them in an overlay.

## Steps
1. Open the command palette and run `Help: Run Doctor`.
2. Scroll the overlay to the end.
3. Press `Esc`.

## Expected
- An overlay titled `Doctor` opens, with sections `core`, `lsp`, `dap`, `terminal`.
- The neovim row is `✓` and shows the `NVIM v…` version.
- Every tool not on `$PATH` is shown with `?` and an install hint.
- The last lines say "All required tools found." and mention `termocode setup`.
- Nothing is installed or downloaded (no new files under `~/.local/share/fonts`).
- `Esc` closes the overlay and the editor is unchanged.

## Actual
_(filled at run time)_

## Result
`⬜ untested`  ·  ✅ pass / ⚠️ partial / ❌ fail / — n/a
