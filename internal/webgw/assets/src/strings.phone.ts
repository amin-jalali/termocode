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
  // Review in chat (MOB-14)
  "rv.title": "Review the agent's work",
  "rv.pick": "Pick an agent worktree to review its changes.",
  "rv.files": "Files the agent changed",
  "rv.file_of": "File {i} of {n}",
  "rv.prev": "Previous file",
  "rv.next": "Next file",
  "rv.same": "No line of this file changed.",
  "rv.secret": "This file may hold secrets, so its lines stay on the laptop.",
  "rv.binary": "Not a text file: there are no lines to show.",
  "rv.cut": "The file is long: only its start is shown.",
  "rv.line_add": "Added line {line}. Long-press or Enter adds a note.",
  "rv.line_del": "Removed line {line}. Long-press or Enter adds a note.",
  "rv.line_ctx": "Line {line}. Long-press or Enter adds a note.",
  "rv.note_on": "Note on {path}, line {line}",
  "rv.note_side_old": "Note on {path}, removed line {line}",
  "rv.note_hint": "What should the agent change here?",
  "rv.note_keep": "Keep note",
  "rv.note_remove": "Remove note",
  "rv.note_open": "Your note on line {line}: {text}. Tap to change it.",
  "rv.swipe": "Swipe sideways for other files. Long-press a line for a note.",
  "rv.offline": "Offline. Your notes wait here until you send them.",
  "rv.notes": "Your notes",
  "rv.notes_max": "That is the most notes one send can carry.",
  "rv.send_one": "Send 1 note to agent",
  "rv.send_n": "Send {n} notes to agent",
  "rv.sending": "Sending the notes…",
  "rv.sent": "Notes sent. The agent reads them in the chat.",
  "rv.running": "The agent still works here. Review it when its step ends.",
  "rv.viewer": "Only the person in control can send notes.",
  "rv.merge_laptop": "Merge (laptop)",
  "rv.discard_laptop": "Discard (laptop)",
} as const;

addStrings(SP);
