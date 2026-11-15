// The Now list from the inbox's Now fields (whose turn, level, due, age),
// in the laptop's fixed order: your turn first; inside, level P1→P4, a
// due date by today's end first, then age (oldest first). Pure: no DOM,
// no timers, no network. An item from an older laptop has no turn: it
// goes to Everything else.
import type { InboxItem, NowRule } from "../../../../webproto/proto";

export interface Sections {
  turn: InboxItem[];
  overdue: InboxItem[];
  waiting: InboxItem[];
  other: InboxItem[];
  running: InboxItem[];
}

const lvl = (l?: number) => (l && l >= 1 && l <= 4 ? l : 4);

function dayStart(now: number): number {
  const d = new Date(now);
  d.setHours(0, 0, 0, 0);
  return d.getTime();
}

/** rank sorts a copy of items in the Now order. */
export function rank(items: readonly InboxItem[], now: number): InboxItem[] {
  const eod = dayStart(now) + 86_400_000;
  const dueRank = (x: InboxItem) => (!x.due ? 2 : x.due < eod ? 0 : 1);
  return [...items].sort(
    (a, b) => lvl(a.level) - lvl(b.level) || dueRank(a) - dueRank(b) || (a.due ?? 0) - (b.due ?? 0) || a.since - b.since || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0),
  );
}

/** sections splits the inbox: overdue (due before today) apart, then by turn. */
export function sections(items: readonly InboxItem[], now: number): Sections {
  const s: Sections = { turn: [], overdue: [], waiting: [], other: [], running: [] };
  const sod = dayStart(now);
  for (const x of rank(items, now)) {
    if (x.state === "working") s.running.push(x);
    if (x.due && x.due < sod && (x.turn === "me" || x.turn === "others")) s.overdue.push(x);
    else if (x.turn === "me") s.turn.push(x);
    else if (x.turn === "others") s.waiting.push(x);
    else s.other.push(x);
  }
  return s;
}

/** summary is the Home's line: what needs the user, what runs. */
export function summary(s: Sections): { need: number; running: number } {
  return { need: s.turn.length + s.overdue.length, running: s.running.length };
}

/** age is a short age: now, 12m, 3h, 1d. */
export function age(since: number, now: number): string {
  const m = Math.floor((now - since) / 60_000);
  if (m < 1) return "now";
  if (m < 60) return m + "m";
  const h = Math.floor(m / 60);
  return h < 24 ? h + "h" : Math.floor(h / 24) + "d";
}

/** ruleName is the name of the rule with id (its id when gone). */
export function ruleName(rules: readonly NowRule[], id: string): string {
  return rules.find((r) => r.id === id)?.name ?? id;
}
