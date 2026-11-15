// The words of the phone's Now (now.js). Short labels only; source names
// (GitHub, Linear, Jira, YouTrack) are the user's own connected sources.
// Pure.
const S = {
  now: "Now",
  sum_need: "{n} need you",
  sum_sub: "Home is Now: only what waits on you",
  new: "New: {title} needs you",
  turn: "Your turn",
  running: "Running",
  waiting: "Waiting",
  waiting_others: "Waiting on others",
  else: "Everything else",
  all: "All",
  see_all: "See all {n} ›",
  overdue: "Overdue",
  tomorrow_all: "Move all to tomorrow",
  clear: "All clear.",
  clear_sub: "Nothing waits on you.",
  why: "Why this?",
  rule_w: "Rule",
  puts: "puts it on top.",
  why_none: "No rule puts it on top.",
  fix: "✦ Fix",
  open: "Open",
  done: "Done",
  later: "Later",
  mute: "Not important",
  edit_rule: "Edit rule",
  undo: "Undo",
  done_t: "Done: {title}",
  later_t: "Later: {title}",
  mute_t: "Muted: {title}",
  later_q: "Later: {title}",
  later_ph: "in 2 hours",
  later_hint: "Comes back on top at that time",
  p_2h: "in 2 hours",
  p_tom: "Tomorrow 9:00",
  p_week: "Next week",
  fix_text: "Fix the failing CI on {name}: read the failed check's log and fix the cause.",
  setup: "Set up Now · 3 questions",
  setup_top: "On top: agents waiting, red CI, reviews",
  setup_from: "From: GitHub, Linear, agents, CI",
  setup_show: "Show {n} at once",
  use: "Use these",
  change: "Change",
  skip: "Skip",
  setup_t: "Set up Now",
  setup_sub: "Asked once. Defaults are set. Change later in Rules.",
  q1: "1 · What goes on top?",
  q2: "2 · Where from?",
  q3: "3 · How many on Home?",
  q4: "4 · Never on top",
  a_agent: "Agents waiting for me",
  a_ci_red: "Red CI on my branches",
  a_review: "Reviews asked of me",
  a_mention: "Mentions",
  a_bots: "Bots and drafts",
  rules: "Rules",
  rules_n: "Rules {n} of 7",
  rules_sub: "Top rule wins.",
  hidden: "{n} hidden",
  from_item: "From an item",
  from_item_sub: "Long-press a row → “Always on top like this”.",
  back: "Back",
  settings: "Rules",
  agent: "Agent",
  agent_run: "Agent run",
  ci: "CI",
  task: "Task",
  workflow: "Workflow",
  race: "Race",
  lane: "lane",
} as const;

export type NowKey = keyof typeof S;

/** s is one of Now's strings with {name} filled in. */
export function s(key: NowKey, vars?: Record<string, string | number>): string {
  let out: string = S[key];
  if (vars) Object.keys(vars).forEach((k) => (out = out.split("{" + k + "}").join(String(vars[k]))));
  return out;
}

/** Source names of "Where from?" in the order the card shows them. */
export const SOURCES: readonly [string, string][] = [
  ["github", "GitHub"],
  ["linear", "Linear"],
  ["jira", "Jira"],
  ["youtrack", "YouTrack"],
  ["agents", "Agents"],
  ["ci", "CI"],
];

/** The first-run defaults (the laptop's now.DefaultAnswers). */
export const DEFAULTS = { top: ["agent", "ci_red", "review"], from: ["github", "linear", "agents", "ci"], show: 5, hide_bots: true };
