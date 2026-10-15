# Find in files, in depth

!!! info "A deep dive for curious users and contributors"
    This page traces one feature through the code. For the how-to, read the
    [Find and replace guide](../guides/search.md).

    Since this page was first written, search gained the `Aa` / `ab` / `.*`
    toggles, include / exclude globs and context lines (live overlay), result
    streaming, and a configurable limit. The numbers below are current:
    the total limit is the `search_max_results` setting (**default 2000**,
    `0` = no limit), and ripgrep still stops at 100 matches per file.

This page traces **everything** that happens when you search across your project in Termocode — from the keystroke that starts it, through the search engine, to the pixels you click on. It is grounded in the actual code (`internal/search/` and `internal/app/find_results*.go`, `internal/search/model.go`).

> **Scope:** this is *workspace* search ("find in files"), not the in-buffer findbar (`Ctrl+F`). The findbar searches the current file via Neovim's own engine and is covered in the [Find and replace guide](../guides/search.md).

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
rg --json --max-count=100 --max-columns=200 --smart-case [--fixed-strings] [--word-regexp] [--context=N] [--glob=…] -- <query>
```

(`--case-sensitive` replaces `--smart-case` when `Aa` is on; `--fixed-strings` is added when regex is off.) It runs with `cmd.Dir = <root>`. Flag by flag:

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

Collection stops at the **`search_max_results`** limit (default **2000**). The summary then reports *Showing N of M*.

### 2.3 The pure-Go fallback (`runFallback`) — works without ripgrep

If `rg` is not on `PATH`, find-in-files **still works** via a dependency-free directory walker. It is deliberately simpler than ripgrep:

- Uses Go's `regexp`: the query is quoted (literal) unless regex mode is on; still **smart-case**.
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
- Accumulates across roots and stops once the combined total hits the limit.

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
3. `search.Stream(ctx, roots, q, opts)` starts the search; results arrive in batches about every 100 ms, and a new search cancels the old one.
4. **Error** → red toast *"Find failed"* with detail. **Zero results** → info toast *"No matches for «query»"*. Either way, no buffer is opened.

### Step 4 — Build the rows (`find_rows.go`)

Each streamed batch is turned into plain text lines plus a **row table**
entry per line (`buildFindRows`). The page looks like this (the icons, fold
marks, count pills, hint bar and the rule are drawn by nvim, not stored):

```
    query                                       Aa  ab  .*   rg
 3 matches across 2 files             Enter open  Tab fold  Esc close
──────────────────────────────────────────────────────────────────────
 ▾  ◆ pkg/util/util.go                                               2
      3   // Add adds.
      4   func Add(a, b int) int { return a + b }

 ▾  ◆ main.go                                                        1
      3   func main() { Add(1, 2) }
```

Details that matter:

- Results are **grouped by file**, first-seen order; within a file, hits are sorted by line number.
- A header line is the display path (workspace-relative; with several folders prefixed by the folder name) after 6 cells of padding for the fold mark and the file icon.
- A match row is `"  %5d   <code>"`: the code has tabs expanded and indentation dropped. If the first match starts far right the row starts with `…` and keeps 24 cells before it; rows are cut at 160 cells with `…`.
- The row table (`_G._termocode_find.rows[lnum]`) holds the kind (`q` query, `s` summary, `r` rule, `h` header, `m` match, `e` empty-state text, empty for a blank line), the absolute file, the line and column, and the **byte ranges of the matches** (from `search.Result.Matches`, shifted for the prefix and the cut). Nothing ever parses the text back.
- Row 2 is the summary (`findSummaryLine`): `Searching… N matches in M files so far` while it streams, then `M matches across N files`, `Showing N of M … · search_max_results` (warning colour) when capped, or `Search failed: …` (error colour).

### Step 5 — Open the page

The first batch opens `‹tmpdir›/termocode-find-results.txt` with `keepalt edit`, renames the buffer to `…/Find Results`, sets `buftype=nofile`, `buflisted`, `modifiable=false`, remembers it in `vim.g.termocode_find_results_buf`, and sets `filetype=findresults` **last** (the FileType autocmd installs the keys and the window look). `stopinsert` is forced (a global `BufEnter * startinsert` fires before `buftype` is set). Then `_G._termocode_find_set(head, lines, rows)` fills it and puts the cursor on the first match; later batches go through `_G._termocode_find_append` (one `ExecLuaArgs` call each — msgpack, no escaping, no temp files).

A refresh (files changed on disk, or a chip click) replaces the page in place; when nothing matches any more, the page shows `No matches for "query"` with the `F8` hint instead of closing.

### Step 6 — Drawing (`find_results_lua.go`)

A **decoration provider** draws everything that is not text with *ephemeral* extmarks, for the visible rows only, at redraw time: the search icon and the query chip, the option chips and engine, the hint bar (pairs drop from the middle when narrow), the lit rule, fold marks, file icons (one highlight group per icon colour), folder / name tints, count pills, the dim line numbers, the match wash and the `…` cut marks. Nothing is stored, so a 2 000-row page has no extmarks and idle costs nothing.

Every colour is a `TermocodeFind*` group defined by the theme (`themeLuaBody`, from `theme.NvimColors`), so the page follows theme switches. The window gets no number gutter or indent guides, `winhighlight` for the cursor row (the selection wash), and `foldmethod=expr` with one fold per file (from the row table).

### Step 7 — Keys and mouse on the page

- **`Enter`** / **`o`** / a click on a match opens the file at the match (line and column). On a header they open the file.
- **`Tab`** and a click on a header fold / unfold that file. A double-click on a header opens the file.
- **`R`** opens Replace in Workspace with the query filled in; **`F8`** on the page opens the Find prompt with it.
- A click on **`Aa` / `ab` / `.*`** searches again with that option flipped (`termocode_find_toggle`).
- **`q`** / **`Esc`** close the page through the app (so the tab strip updates at once).
- `<LeftDrag>` / `<LeftRelease>` are `<Nop>`: a click never becomes a stuck selection.

### Step 8 — Global next/previous match (`F4` / `Shift+F4`)

`F4` / `Shift+F4` are termocode keymap actions (`NextSearchResult` / `PrevSearchResult`, also in the palette as *Search: Go to Next / Previous Search Result*). They work in every mode. The app calls `_G._termocode_find_nav(dir)`, which walks the **row table** from the page's cursor (or the last jump) to the next / previous match row, skipping folded files, moves the page's cursor there and opens the match. The first `F4` after a search opens the match under the cursor. It returns `'none'` without a page and `'end'` when there is no more match; the app shows a toast for both.

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
- **Regex:** on by default for the `F8` buffer (when `rg` is installed); a toggle (`.*`) in the live overlay.
- **Scope:** every workspace root (primary cwd + added folders).
- **Ignore rules:** ripgrep honors `.gitignore`/`.ignore`; the fallback instead prunes a fixed set of dependency/VCS/dot directories.
- **Caps:** ≤ `search_max_results` results total (default 2000), ≤ 100 matches per file (rg), lines over 200 columns truncated, files over 5 MiB skipped (fallback), binaries skipped.

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

**Inside the results buffer:** `<CR>`/click = open hit.

**Inside the live overlay:** `↑↓`/`Ctrl+P`/`Ctrl+N` = move · `Enter` = open · `Esc` = close.

**Engine knobs:** `rg --json --smart-case --max-count=100 --max-columns=200`; `search_max_results` cap (default 2000); 150 ms overlay debounce; 5 MiB / binary / dot-dir skips on the fallback.

**Key files:** `internal/search/search.go` (engine), `internal/search/model.go` (live overlay), `internal/app/find_results.go` (buffer flow), `internal/app/find_results_lua.go` (buffer syntax + keymaps).

---

*Related: in-buffer find/replace (`Ctrl+F` / `Ctrl+H`) and workspace replace (`Ctrl+Shift+H`) are covered in the [Find and replace guide](../guides/search.md).*
