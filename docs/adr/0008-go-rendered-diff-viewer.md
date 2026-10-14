# ADR 0008 — A Go-rendered diff card for reviewing changes

Status: **Accepted** (2026-10-15)

## Context
AI changes must be reviewed before they are written (Group R, a port of
mobocode's diff viewer and AI review card). The review needs: side by side
and unified views, word-level highlights inside changed lines, folded
unchanged runs, Accept / Reject buttons on every change, per-file buttons,
and a list of files when one AI turn changes several.

Two ways were on the table:

1. **nvim diff mode** (what the git side-by-side diff and the old AI
   proposal used): two windows with `diffthis`. It has syntax colours and
   real editing, but it has no unified view, no buttons inside the text,
   only line-level (plus a coarse `diffopt=inline`) highlights, and it
   changes the user's window layout.
2. **A card drawn by termocode** from a diff computed in Go.

## Decision
The review uses a card drawn in Go: `internal/textdiff` (port of
mobocode's `text_diff.dart`: LCS line diff with prefix/suffix trimming and
a size cap, paired modify rows with word spans, hunks, apply a subset of
hunks) and `internal/diffview` (the card: header, Current / Proposed
labels, split or unified body, "N unchanged lines" bands, overview ruler,
change headers with Accept / Reject, file list, footer buttons). The app
glue is `internal/app/ai_review.go`. "File: Compare Two Files…" uses the
same card read-only.

The git side-by-side diff keeps nvim diff mode: there the user edits and
stages in place, which the card can not do.

## Consequences
- **+** The same look as mobocode, with buttons where the change is; the
  window layout is never touched; it works over SSH like the rest.
- **+** The engine is pure Go and unit-tested (every hunk subset rebuilds
  the expected file).
- **−** No syntax colours in the card (mobocode does not colour its diff
  either: the add / remove colours dominate).
- **−** Two diff UIs exist (card for review, nvim for git).
