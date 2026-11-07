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
  // Home context chips (MOBUX-3)
  "chips.label": "Shortcuts",
  "chips.needs": "Needs you · {n}",
  "chips.clear": "Nothing waits",
  "chips.board": "Board",
  "chips.research": "Research",
  "chips.mcp": "MCP",
  "chips.whatcan": "What can I do?",
  "chips.edit": "Pin chips",
  "chips.body_hint": "Tap up to 3 chips to keep them first. Tap again to unpin.",
  "chips.full": "Up to 3 chips stay pinned. Unpin one first.",
  "chips.pinned_name": "{name}, pinned",
} as const;

addStrings(SP);
