// The race strings of the phone app (MOB-62). The phone app is English
// only (owner 2026-11-07), so these stay out of strings.ts and its
// Persian twin. Same voice as strings.ts: calm, short, present tense,
// "the laptop", no other product named. {name} marks a value.
export const RS = {
  "race.summarise": "Summarise",
  "race.summarising": "Summarising…",
  "race.verify": "Verify",
  "race.stop": "Stop lanes",
  "race.stop_body": "Stop the lanes that still work? Each agent stops its turn. The work stays in the worktrees.",
  "race.summary": "Difference summary",
  "race.hint_summary": "Summarise needs two finished lanes.",
  "race.hint_verify": "This race has no verify command.",
  "race.start": "Start a race",
  "race.task": "Task for every agent",
  "race.lanes": "Lanes, 2 to 4",
  "race.lane_n": "Lane {n}",
  "race.add_lane": "Add a lane",
  "race.remove_lane": "Remove lane {n}",
  "race.verify_cmd": "Verify command, optional",
  "race.start_body": "Start a race of {n} lanes? Each agent works in its own git worktree on the laptop. Your files change only when you merge a lane.",
  "race.need_task": "Type the task for the agents.",
  "race.need_lanes": "A race needs 2 to 4 lanes.",
  "race.one_line": "The verify command is one line.",
} as const;

export type RaceKey = keyof typeof RS;

/** rs is the text of key with its {name} values filled in. */
export function rs(key: RaceKey, vars?: Record<string, string | number>): string {
  const text: string = RS[key];
  if (!vars) return text;
  return text.replace(/\{([a-z_]+)\}/g, (m, name: string) => (name in vars ? String(vars[name]) : m));
}
