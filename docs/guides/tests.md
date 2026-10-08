# Run and explore tests

The **Testing** view finds the tests in your project and shows them as a
tree: package → file → test. You can run one test, a file, or everything.

Supported frameworks:

| Project file | Framework | Runs |
|---|---|---|
| `go.mod` | Go | `go test -json` |
| `Cargo.toml` | Rust | `cargo test` |
| `pyproject.toml`, `setup.py`, `pytest.ini`, `setup.cfg`, `tox.ini` | pytest | `pytest` with a JUnit XML report |
| `package.json` | Jest or Vitest | `npx jest` / `npx vitest run` |

termocode picks Vitest when `package.json` mentions it, and Jest otherwise.

## Run all tests

1. Press `Alt+T` (or run **Testing: Run All Tests**).
2. The Testing view opens. Each test gets a mark while it runs and when it is
   done: ✓ passed, ✗ failed, ⊘ skipped.
3. The status bar shows the totals, like `✓ 41 ✗ 2`.

## Run one test or one file

- In the editor: put the cursor in a test and run
  **Testing: Run Test at Cursor**. Or run **Testing: Run Tests in Current File**.
- In the Testing view: move to a test, file or package and press `r`.
- In Go files, `Ctrl+.` also offers **Run Test: `<Name>`**.

## Read a failure

1. Failed tests show their message right under the test row.
2. Press `Enter` on a failed test to jump to the line that failed.
3. Press `o` to see the full output of that test in the **TEST RESULTS** tab.

Run **View: Test Results** to open that tab any time.

## Rerun only the failures

Press `f` in the Testing view, or run **Testing: Rerun Failed Tests**.

## Open the Testing view

Click the **Testing** icon in the activity bar, or run
**Testing: Focus Test Explorer**. Press `u` (or run **Testing: Refresh Tests**)
after you add new tests.

## Keys at a glance

| Key | Action |
|---|---|
| `Alt+T` | Run all tests |

In the Testing view:

| Key | Action |
|---|---|
| `j` / `k` | Move |
| `h` / `l` or `←` / `→` | Collapse / expand |
| `Space` | Toggle fold |
| `Enter` | Open the test (or the failure line) |
| `r` | Run the item under the cursor |
| `o` | Show its output |
| `R` | Run all |
| `f` | Rerun failed |
| `u` | Refresh the tree |
| `c` | Collapse all |
| `x` | Stop the run |
| `Esc` | Back to the editor |

In the TEST RESULTS tab: `j` / `k` scroll, `g` / `G` top / bottom, `a` shows
the output of all tests again.

## Troubleshooting

**The tree is empty**
:   termocode did not find a marker file in the workspace root. Open
    termocode in the project root (where `go.mod` or `package.json` is).

**Jest / Vitest: "command not found"**
:   termocode runs `npx --no-install`, so the tool must be installed in the
    project: `npm install --save-dev jest` (or `vitest`).

**pytest runs, but no results show**
:   termocode reads the JUnit XML report. Check that `pytest` is the one in
    your active virtual environment.

**`Alt+T` does nothing**
:   Your terminal may not send `Alt`. Use **Testing: Run All Tests**.

**The run never ends**
:   Press `x` in the Testing view, or run **Testing: Stop Test Run**.
