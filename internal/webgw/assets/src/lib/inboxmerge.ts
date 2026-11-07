// The inbox across projects (MOB-53, mobile plan 3.7). The page
// subscribes to `inbox` once per open project (`ws` on the sub, MOB-50);
// the door stamps `ws` on each snap. This merges them into the one Home
// stream with the same rules as webproto.MergeInbox on the laptop: the
// frame's ws wins over an item's own, the same ws + id twice is one item
// (the newer copy), newest first (ties by ws, then id), at most 200 with
// `more` counting the rest. Pure; for the phone's Home chunk only.
import type { InboxCounts, InboxItem, InboxSnap } from "../../../../webproto/proto";

export const MAX_INBOX = 200;
const MAX_NAME = 24;
const NAMES_SHOWN = 3;
const KINDS = new Set(["card", "run", "lane", "workflow", "task", "ci", "race"]);
const STATES = ["needs_you", "working", "failed", "done"] as const;

/** validWS: a workspace key, 16 lower-case hex digits. */
export function validWS(ws: string): boolean {
  return /^[0-9a-f]{16}$/.test(ws);
}

function zero(): InboxCounts {
  return { needs_you: 0, working: 0, failed: 0, done: 0 };
}

function bump(c: InboxCounts, state: string, n: number): void {
  if ((STATES as readonly string[]).includes(state)) c[state as keyof InboxCounts] += n;
}

function itemOK(it: InboxItem): boolean {
  return typeof it?.id === "string" && it.id !== "" && KINDS.has(it.kind) && (STATES as readonly string[]).includes(it.state);
}

/** mergeInbox merges each project's inbox (keyed by ws; "" is home). */
export function mergeInbox(home: string, parts: ReadonlyMap<string, InboxSnap | undefined>): InboxSnap {
  const counts = zero();
  let more = 0;
  const at = new Map<string, number>();
  const items: InboxItem[] = [];
  for (const [raw, snap] of parts) {
    const ws = raw === "" ? home : raw;
    if (!validWS(ws) || !snap) continue;
    for (const s of STATES) counts[s] += Math.max(snap.counts?.[s] ?? 0, 0);
    more += Math.max(snap.more ?? 0, 0);
    for (const src of snap.items ?? []) {
      if (!itemOK(src)) {
        bump(counts, src?.state, -1);
        continue;
      }
      const it = { ...src, ws };
      const k = ws + "\u0000" + it.id;
      const i = at.get(k);
      if (i === undefined) {
        at.set(k, items.length);
        items.push(it);
      } else if (it.since > items[i].since) {
        bump(counts, items[i].state, -1);
        items[i] = it;
      } else {
        bump(counts, it.state, -1);
      }
    }
  }
  items.sort((a, b) => b.since - a.since || cmp(a.ws ?? "", b.ws ?? "") || cmp(a.id, b.id));
  if (items.length > MAX_INBOX) {
    more += items.length - MAX_INBOX;
    items.length = MAX_INBOX;
  }
  for (const s of STATES) counts[s] = Math.max(counts[s], 0);
  return more > 0 ? { items, counts, more } : { items, counts };
}

function cmp(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}

/** projectName: the last path element, no control characters, ≤ 24 characters. */
export function projectName(name: string | undefined): string {
  let s = (name ?? "").split(/[/\\]/).pop() ?? "";
  s = Array.from(s.replace(/\p{C}/gu, "")).slice(0, MAX_NAME).join("").trim();
  return s === "" ? "a project" : s;
}

/** needsYouLine: "1 thing needs you · shop", "3 things need you · shop, api"; "" when nothing waits. */
export function needsYouLine(s: InboxSnap, names: ReadonlyMap<string, string>): string {
  const order: string[] = [];
  for (const it of s.items) {
    const ws = it.ws ?? "";
    if (it.state === "needs_you" && !order.includes(ws)) order.push(ws);
  }
  const n = Math.max(s.counts.needs_you, order.length);
  if (n === 0) return "";
  let line = n === 1 ? "1 thing needs you" : `${n} things need you`;
  if (order.length === 0) return line;
  const shown = order.slice(0, NAMES_SHOWN).map((ws) => projectName(names.get(ws)));
  line += " · " + shown.join(", ");
  if (order.length > NAMES_SHOWN) line += ` +${order.length - NAMES_SHOWN}`;
  return line;
}

/** newNeedsYou: items of next that need the user and did not in prev. */
export function newNeedsYou(prev: InboxSnap | undefined, next: InboxSnap): InboxItem[] {
  const had = new Set<string>();
  for (const it of prev?.items ?? []) if (it.state === "needs_you") had.add((it.ws ?? "") + "\u0000" + it.id);
  return next.items.filter((it) => it.state === "needs_you" && !had.has((it.ws ?? "") + "\u0000" + it.id));
}

/** itemLink: a card opens its sheet on Home, anything else its item; ws names the project. */
export function itemLink(it: InboxItem): string {
  const q = new URLSearchParams();
  if (it.kind === "card") q.set("card", it.ref);
  else q.set("item", it.id);
  if (it.ws) q.set("ws", it.ws);
  return "#/home?" + q.toString();
}
