// The strings only the desktop workbench shows (web UX 7.4), in the same
// form and voice as strings.ts. Only workbench.tsx imports this file, so
// it is bundled into workbench.js and a phone never loads it. A new
// string goes into strings.ts; strings_desk_test.go says when one is used
// only by the workbench and belongs here. Persian texts of these keys
// stay in strings.fa.ts.
import { addStrings } from "./strings";

export const SD = {
  // Workflows (desktop side bar and run tab)
  "wfv.list": "All workflows",
  "wfv.no_runs": "No runs yet.",
  "wfv.on": "on",
  "wfv.off": "off",
  "wfv.untrusted": "not trusted yet",
  "wfv.broken": "the file has problems",
  "wfv.interrupted": "stopped by a restart",
  "wfv.dry": "dry run",
  "wfv.items": "{n} items",
  "wfv.item_one": "1 item",
  "wfv.failed": "{n} failed",
  "wfv.wrote": "wrote {n}",
  "wfv.resumed": "resumed after a restart",
  "wfv.started": "started {time}",
  "wfv.needs": "Needs you",
  "wfv.last_lines": "Last lines",
  "wfv.body_laptop": "Run, turn on or off, edit and allow a workflow on the laptop.",
  "wfv.gone": "This run is no longer in the list.",
  "wfv.tab": "{name} (Run)",
} as const;

addStrings(SD);
