# Find in Files — The Full Story

This document traces **everything** that happens when you search across your project in Termocode — from the keystroke that starts it, through the search engine, to the pixels you click on. It is grounded in the actual code (`internal/search/` and `internal/app/find_results*.go`, `internal/search/model.go`).

> **Scope:** this is *workspace* search ("find in files"), not the in-buffer findbar (`Ctrl+F`). The findbar searches the current file via Neovim's own engine and is covered in the main feature doc.

---

## 1. The big picture: two surfaces, one engine

Termocode exposes find-in-files through **two completely different front-ends** that share **one back-end search engine**:

```
                       ┌─────────────────────────────────────────┐
   F8 / Alt+F   ─────▶ │  A. RESULTS BUFFER  (Sublime-style)      │
   palette            │     prompt → scratch buffer "Find Results"│
   "Find in Files"    └─────────────────────────────────────────┘
                                        │
                                        ▼
                       ┌─────────────────────────────────────────┐
                       │   THE ENGINE  (internal/search)          │
                       │   ripgrep --json   ·   pure-Go fallback  │
                       └─────────────────────────────────────────┘
                                        ▲
   palette            ┌─────────────────────────────────────────┐
   "Find in Files     │  B. LIVE OVERLAY  (floating picker)      │
   (Live Overlay)" ──▶│     debounced, updates as you type       │
                       └─────────────────────────────────────────┘
```

| | **A. Results buffer** | **B. Live overlay** |
|---|---|---|
| How to open | `F8` / `Alt+F`, palette *Search: Find in Files*, welcome card | palette *Search: Find in Files (Live Overlay)* |
| Query input | a one-line **prompt** (submit once) | a **floating box**, searches as you type |
| Results shown in | a real Neovim **scratch buffer** you can scroll/keep | a transient **picker** overlay |
| Updates | one shot per submit | live, **150 ms debounced** |
| Persists after use | yes — the buffer stays until closed | no — closes on select/Esc |
| Navigation | `<CR>`/click in buffer; `F4`/`Shift+F4` globally | `↑↓`, `Enter`, `Esc` |
| Best for | reviewing many hits, jumping back and forth | quick "jump to the one match" |

Both call the same functions in `internal/search`, so their **matching semantics are identical**.

---

## 2. The shared engine (`internal/search/search.go`)

Everything funnels through three functions:

- `Run(dir, query)` — single directory; keeps paths relative.
- `RunDirs(dirs, query)` — **multi-root**; rewrites every hit's path to absolute so it opens regardless of which root it came from. *This is what both front-ends actually call.*
- `runOne(dir, query)` — the worker that shells out to ripgrep.

### 2.1 The ripgrep invocation

```
rg --json --smart-case --max-count=100 --max-columns=200 -- <query>
```

run with `cmd.Dir = <root>`. Flag by flag:

| Flag | Effect |
|---|---|
| `--json` | emit a structured event stream (one JSON object per line) instead of plain text |
| `--smart-case` | case-**insensitive** unless the query contains an uppercase letter |
| `--max-count=100` | stop after 100 matches **per file** |
| `--max-columns=200` | replace lines longer than 200 cols with a placeholder (keeps minified files from exploding the UI) |
| `--` | terminate options; the query is taken **literally** (it is *not* treated as a flag, and is not regex-escaped by us — so ripgrep's own regex syntax is available) |

**Exit-code handling:** ripgrep returns exit code `1` when there are simply **no matches** — this is mapped to `(nil, nil)`, *not* an error. Any other non-zero exit surfaces the first line of stderr as an error.

### 2.2 Parsing the JSON stream (`parseJSON`)

The stream is scanned line by line (with a **1 MiB per-event buffer** to survive long minified lines). Only events of `type == "match"` are kept; `begin`/`end`/`summary` envelopes are ignored. For each match it extracts:

- **path** (`data.path.text`)
- **line number** (`data.line_number`, 1-based)
- **column** = the start of the *first* submatch + 1
- **preview** = the raw line text (trailing `\r\n` stripped, otherwise untrimmed)
- **matches** = every submatch as a `{Start, End}` byte range, so the renderer can bold **all** matched spans on the line, not just the first

Results are a slice of:

```go
type Result struct {
    Path    string       // relative (Run) or absolute (RunDirs)
    Line    int          // 1-based
    Col     int          // 1-based, first match on the line
    Preview string       // raw line text
    Matches []MatchRange // byte ranges to highlight
}
```

Collection stops at **`MaxResults = 100`** total.

### 2.3 The pure-Go fallback (`runFallback`) — works without ripgrep

If `rg` is not on `PATH`, find-in-files **still works** via a dependency-free directory walker. It is deliberately simpler than ripgrep:

- **Literal** substring search only (no regex), still **smart-case**.
- Walks the tree with `filepath.WalkDir`, pruning directories via `skipSearchDir`: `.git`, `node_modules`, `vendor`, `target`, `dist`, `build`, `.next`, `.cache`, `__pycache__`, `.venv`, `venv`, and **any dot-directory**.
- Skips files larger than **5 MiB** (`maxFallbackFileSize`).
- Skips **binary** files — detected by a NUL byte in the first 8 KiB (`isBinary`).
- Produces the **exact same `Result` shape** (including every match span per line and the 200-column truncation) so the renderers can't tell the difference.
- **No `.gitignore` awareness** — that's the one feature you gain back by installing ripgrep.

The live overlay flags this state by appending **`· basic`** to its summary line; installing `rg` silently upgrades to the full engine.

### 2.4 Multi-root behavior (`RunDirs`)

- Empty / whitespace-only query → short-circuits to `(nil, nil)` *before* spawning anything.
- Iterates each root, running `runOne` (or the fallback) per directory.
- **One bad root doesn't kill the search:** an error from a root is remembered but the others continue; the error is only surfaced if **no** root produced any results.
- Each hit's relative path is rewritten to **absolute** (`absJoin`) so it opens correctly no matter which root matched.
- Accumulates across roots and stops once the combined total hits **100**.

The roots themselves come from `workspaceRoots()` — the primary cwd plus any folders added via *Workspace: Add Folder…*.

---

## 3. Surface A — the Results buffer (the full flow)

This is the `F8` / `Alt+F` experience. Walk it top to bottom.

### Step 1 — Entry point

`F8` and `Alt+F` are both bound to `ActionWorkspaceSearch` (`keymap.go`). The handler (`update.go`) and the palette item *Search: Find in Files* both call **`openFindInFilesPrompt()`** (`find_results.go`). The welcome-screen "Find in files" card dispatches the same action.

### Step 2 — The query prompt

`openFindInFilesPrompt()` opens the standard one-line prompt overlay titled **"Find in Files"** with label **"Find:"**. If you have a Visual-mode selection, it's used as the **initial value** (same prefill UX as the findbar). State is marked `promptKind = promptKindFindInFiles`.

### Step 3 — Submit → run the search

When you press Enter, `handlePromptSubmit` (via `file_ops.go`, which routes `promptKindFindInFiles`) calls **`runFindInFiles(query)`** (`find_results.go`):

1. Trims the query; empty → no-op.
2. `roots := workspaceRoots()`.
3. `results, err := search.RunDirs(roots, q)`.
4. **Error** → red toast *"Find failed"* with detail. **Zero results** → info toast *"No matches for «query»"*. Either way, no buffer is opened.

### Step 4 — Format the results body (`formatFindResults`)

On success the results are rendered into a Sublime-style text body:

```
Searching 3 files for "query"

/abs/path/to/file.go (2):
      42:    matched line content
      45:    another match

/abs/path/to/other.go (1):
      13:    yet another match

3 matches across 3 files
```

Details that matter:

- Results are **grouped by file**, preserving first-seen order; within a file, hits are sorted by line number.
- Each file header is `"<abs path> (N):"` where **N** is that file's hit count.
- Each match row is `"    %4d:    <content>"` — 4-space indent, line number right-padded to 4, a **`:` sigil**, then a 4-space gap before the content. (The format reserves a space sigil `" "` for context rows; the click/nav code uses `:`-vs-` ` to tell match rows from context rows.)
- Preview content is truncated to **250 runes** with a `…` suffix.
- Footer: `"M matches across N files"` (correctly pluralized). A truly empty set would render `0 matches for "query"`, but in practice the zero case is intercepted by the toast in Step 3.

### Step 5 — Open it as a scratch buffer

The body is written to `‹tmpdir›/termocode-find-results.txt`, then a Lua chunk opens it and turns it into a proper results pane:

- `keepalt edit` the temp file, then rename the buffer to `…/Find Results`.
- Set `buftype=nofile`, `swapfile=false`, `buflisted=true`, `modifiable=false`.
- Stash the query in `vim.g.termocode_find_query` (re-set every search so re-running re-paints the highlight).
- Set `filetype=findresults` **last** — this is the trigger that fires the FileType autocmd which paints syntax and installs key maps.
- `stopinsert` is forced immediately. (There's a global `BufEnter * startinsert` autocmd that fires *before* `buftype` is set, so without this you'd land in insert mode on a non-modifiable buffer and your first `<Enter>` would be swallowed — this is a deliberate fix.)
- Remember the buffer id in `vim.g.termocode_find_results_buf` so `F4`/`Shift+F4` can navigate it from anywhere.
- Focus switches to the editor.

### Step 6 — Syntax highlighting (`find_results_lua.go`)

The `FileType=findresults` autocmd paints the buffer using VSCode Dark+ colors so it feels like a sibling of the editor:

| Element | Group | Color |
|---|---|---|
| File path header | `findResultsHeader` | teal `#4ec9b0`, bold |
| The `(N)` hit count | `findResultsHeaderCount` | dim grey `#6c7080`, italic |
| Line-number column | `findResultsLineNum` | gutter grey `#858585` |
| Footer / "Searching…" line | `findResultsFooter` | comment green `#6a9955`, italic |
| Every literal match of the query | `findResultsMatch` | dark-green **background** `#2a3f24` (foreground unchanged, so it reads as "tinted content") |
| Cursor row | `CursorLine` | `#23262e` |

The per-query highlight is built by escaping the query and adding a `syntax match findResultsMatch /\V…/` rule, so **every occurrence** of the search term is tinted, not just the matched column.

### Step 7 — Jumping to a result

Inside the buffer:

- **`<CR>`** (or a click) runs `jump_from_cursor`: it reads the current line, and
  - if it's a **header** (`…:$`), it `:edit`s that file (after `strip_count` removes the trailing `" (N)"` — otherwise it'd try to open the literal `"/path (3)"`);
  - if it's a **match row** (`^\s+NNN[: ] …`), it grabs the line number and scans **upward** to the nearest header, then `:edit +NNN <file>`.
- **Mouse** is special-cased: `<LeftMouse>` and `<2-LeftMouse>` both position-and-jump, while `<LeftDrag>` and `<LeftRelease>` are mapped to `<Nop>`. This fixes a reported bug where a single click in the buffer turned into a stuck visual-drag selection.

### Step 8 — Global next/previous match (`F4` / `Shift+F4`)

These are bound **globally** (normal *and* insert mode), so you can cycle matches without returning to the results buffer:

- `navigate_results(+1)` / `navigate_results(-1)` walk the saved results buffer (`vim.g.termocode_find_results_buf`) from the currently-selected row, looking for the next/previous **match row** (identified by the `:` sigil — context rows with a space sigil are skipped).
- On finding one, it moves the cursor in the results window (visual feedback) and `:edit +line <file>` to jump there.

---

## 4. Surface B — the Live Overlay (the full flow)

This is the palette-only *Search: Find in Files (Live Overlay)*. It's a self-contained Bubble Tea component (`internal/search/model.go`).

### Step 1 — Open

The palette case `find-files-overlay` constructs `search.New()`, sizes it, calls `SetRoots(m.explorer.Roots())` to wire in **all** workspace roots, and sets `m.searchOpen = true`. (`New()` records whether ripgrep is available up front.)

### Step 2 — Type to search (debounced, race-safe)

Each keystroke (`Runes`/`Space`/`Backspace`) appends to the input and calls `scheduleSearch()`:

- A monotonically increasing **`queryVer`** is bumped on every keystroke.
- An empty query clears results immediately (no spawn).
- Otherwise a **150 ms `tea.Tick`** is scheduled carrying the current version.

When the tick fires, it only runs if its version still matches the latest `queryVer` — so **stale searches are dropped** and only the most recent keystroke actually spawns ripgrep. The roots are snapshotted before spawning to avoid racing a concurrent `SetRoots`. The async result message is likewise ignored if its version is stale. This is how the overlay stays responsive while you type fast.

### Step 3 — The rendered box

A centered, rounded box (width capped at 110, height at 30) containing:

```
 Find in Files
 > query▍
 12 results
   relpath                              :42:7
     the matched line with the span bolded
   ...
 ↑↓ navigate · enter open · esc close
```

- **Title** ` Find in Files`, then the **`>` input** with a `▍` cursor.
- A **summary line** that reflects state: `type to search` / `searching...` / `no results` / `N results`, plus `· basic` (or `· basic (install rg for regex)`) when running on the fallback engine, or the error in red.
- **Two lines per result**: a header `relpath  :line:col` and an indented snippet. Each matched span in the snippet is **bolded amber** (`#f9c859`); the selected row uses a blue highlight palette (`#094771`). Long paths and snippets are truncated to fit with `…`, and match spans that fall past the truncation are dropped silently (`renderHighlighted` / `truncateBudget`).

### Step 4 — Navigate and select

| Key | Action |
|---|---|
| `↑` / `Ctrl+P` | move selection up |
| `↓` / `Ctrl+N` | move selection down |
| `Enter` | open the selected result |
| `Esc` / `Ctrl+C` | close the overlay |

`Enter` emits a `SelectMsg{Path, Line}`; the app handler (`update.go`) closes the overlay, makes the editor window current, resolves the path to absolute if needed, and runs `edit +<line> <abs>`, then focuses the editor. `Esc` emits `CloseMsg`, which just closes the overlay.

---

## 5. Matching semantics (shared by both surfaces)

Because both surfaces call `RunDirs`, these rules are **identical** everywhere:

- **Case:** smart-case — lowercase query = case-insensitive; any uppercase = case-sensitive.
- **Regex:** available through ripgrep when `rg` is installed (the query is passed after `--` untouched); **literal-only** on the pure-Go fallback.
- **Scope:** every workspace root (primary cwd + added folders).
- **Ignore rules:** ripgrep honors `.gitignore`/`.ignore`; the fallback instead prunes a fixed set of dependency/VCS/dot directories.
- **Caps:** ≤ 100 results total, ≤ 100 matches per file (rg), lines over 200 columns truncated, files over 5 MiB skipped (fallback), binaries skipped.

---

## 6. Edge cases & failure modes

| Situation | What happens |
|---|---|
| Empty / whitespace query | short-circuits before spawning anything; nothing opens |
| No matches | rg exit code 1 → `(nil, nil)`; buffer flow shows toast *"No matches for «q»"*, overlay shows *no results* |
| ripgrep not installed | pure-Go fallback runs; overlay shows `· basic` |
| ripgrep errors (bad regex, etc.) | first stderr line surfaced; buffer flow → *"Find failed"* toast, overlay → red summary |
| One workspace root unreadable | that root is skipped; other roots still searched; error only shown if **all** produced nothing |
| Very long lines (minified) | truncated to 200 cols by the engine, then to 250 runes in the buffer / fit-to-width in the overlay |
| Binary files | skipped (fallback NUL-byte probe; rg skips by default) |
| Single click in results buffer | position-and-jump only — drag/release are `<Nop>` so it never becomes a stuck selection |
| Re-entering the results tab | a buffer-local `BufEnter/WinEnter` forces `stopinsert` so you never get stuck in insert mode on the read-only buffer |

---

## 7. Quick reference

**Open:**
- Results buffer: `F8`, `Alt+F`, palette *Search: Find in Files*, welcome card.
- Live overlay: palette *Search: Find in Files (Live Overlay)*.

**Inside the results buffer:** `<CR>`/click = open hit · `F4`/`Shift+F4` = next/prev match (global).

**Inside the live overlay:** `↑↓`/`Ctrl+P`/`Ctrl+N` = move · `Enter` = open · `Esc` = close.

**Engine knobs:** `rg --json --smart-case --max-count=100 --max-columns=200`; 100-result cap; 150 ms overlay debounce; 5 MiB / binary / dot-dir skips on the fallback.

**Key files:** `internal/search/search.go` (engine), `internal/search/model.go` (live overlay), `internal/app/find_results.go` (buffer flow), `internal/app/find_results_lua.go` (buffer syntax + keymaps).

---

*Related: in-buffer find/replace (`Ctrl+F` / `Ctrl+H`) and workspace replace (`Ctrl+Shift+H`) are documented in `FEATURE.md` §4.*
