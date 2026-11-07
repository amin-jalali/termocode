// The strings only the phone app's lazy views show (mobile plan, MOB-3
// on): typed blocks, the Home and its views. Same form and voice as
// strings.ts (strings_test.go lints them too). English only (owner
// 2026-11-07): strings.fa.ts does not translate these keys, so t()
// shows the English text in every language. Only lazy phone chunks and
// the dev kit import this file, so app.js does not grow.
import { addStrings } from "./strings";

export const SP = {
  // Typed blocks from the laptop (MOB-3)
  "blk.unknown": "This block is new to this page.",
  "blk.more": "Showing {shown} of {total}",
  "blk.stale": "Last data, offline",
  "blk.live": "Live",
  "blk.empty": "Nothing here yet.",
  "blk.tone.ok": "OK",
  "blk.tone.warn": "Needs a look",
  "blk.tone.bad": "Not OK",
  "blk.tone.busy": "In progress",
  "blk.tone.idle": "Not started",
  "blk.tone.info": "For your info",
  "blk.count": "Count: {n}",
  "blk.chart_bar": "{label}: {value}",
  "blk.diff_stat": "+{adds} −{dels}",
  "blk.raw": "Raw data",
  "blk.form_send": "Send to the laptop",
  "blk.form_laptop": "Fill this in on the laptop",
  // Saved and pinned views on the Home (MOB-71)
  "saved.title": "Pinned views",
  "saved.unpin": "Unpin view {name}",
  "saved.unpinned": "{name} is no longer pinned here.",
} as const;

addStrings(SP);
