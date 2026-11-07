// The strings only the phone views show (phone.js, MOB-14 on), in the
// same form and voice as strings.ts. Only phone.tsx imports this file,
// so it is bundled into phone.js and the first files never carry it.
// The phone app is English only (owner 2026-11-07): these keys have no
// Persian texts.
import { addStrings } from "./strings";

export const SP = {
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
  "rv.line_add": "Added line {line}. Tap to add a note.",
  "rv.line_del": "Removed line {line}. Tap to add a note.",
  "rv.line_ctx": "Line {line}. Tap to add a note.",
  "rv.note_on": "Note on {path}, line {line}",
  "rv.note_side_old": "Note on {path}, removed line {line}",
  "rv.note_hint": "What should the agent change here?",
  "rv.note_keep": "Keep note",
  "rv.note_remove": "Remove note",
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
