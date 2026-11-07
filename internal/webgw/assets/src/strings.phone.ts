// The strings only the phone's own views show (the mobile app, MOB /
// MOBUX), in the same form and voice as strings.ts. Only phone.tsx
// imports this file, so it is bundled into phone.js and the first files
// of the page never carry it. The phone app is English only (owner
// 2026-11-07): these keys have no Persian text, and t() falls back to
// English for them.
export const SP = {
  // Project page (MOBUX-7)
  "pp.tabs": "Project views",
  "pp.board": "Board",
  "pp.open_n": "{title} · {n} open",
  "pp.no_tasks_body": "No checklist, issues or pull requests in this project yet.",
  "pp.tasks_err": "The tasks did not load. {reason}",
  "pp.talk": "Talk about it",
  "pp.no_runs": "No AI runs in this project yet.",
  "pp.other_body": "Runs, Git and the board show here for the project open in the laptop window. Open it there to see them.",
  "pp.gone": "This project is not on the laptop's list now.",
  "pp.git_none": "This project is not a git repository.",
  "pp.changes": "Changed files: {n}",
  "pp.git_open": "Open in Git",
  "pp.col.working": "Working",
  "pp.checks": "{mark} checks",
  "pp.by": "by {name}",
  "pp.here": "Open in the laptop window",
  "pp.asleep": "Asleep on the laptop",
  "pp.col_none": "None in this column.",
} as const;
