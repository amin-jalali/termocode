// The Run, Test and Debug topics as the views read them (WEBP-4): the
// tests tree, the debug session, the project services. Pure: no DOM,
// timers or network. The hub sends whole snapshots; a reader only checks
// the shape and gives the views what they draw.
import type { DebugSnap, ServicesSnap, TestItem, TestsSnap } from "../../../../webproto/proto";

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);

/** readTestsSnap is the tests topic (null: not here yet or odd). */
export function readTestsSnap(data: unknown): TestsSnap | null {
  return isObj(data) && Array.isArray(data.items) ? (data as unknown as TestsSnap) : null;
}

/** readDebugSnap is the debug topic (null: not here yet or odd). */
export function readDebugSnap(data: unknown): DebugSnap | null {
  return isObj(data) && Array.isArray(data.frames) && Array.isArray(data.breakpoints) && Array.isArray(data.console) ? (data as unknown as DebugSnap) : null;
}

/** readServicesSnap is the services topic (null: not here yet or odd). */
export function readServicesSnap(data: unknown): ServicesSnap | null {
  return isObj(data) && Array.isArray(data.services) ? (data as unknown as ServicesSnap) : null;
}

/** TestNode is one row of the tests tree with its children. */
export interface TestNode {
  item: TestItem;
  kids: TestNode[];
}

/** testTree builds the tree from the topic's flat list (parents come
 * before their children). */
export function testTree(items: readonly TestItem[]): TestNode[] {
  const byID = new Map<string, TestNode>();
  const roots: TestNode[] = [];
  for (const it of items) {
    const n: TestNode = { item: it, kids: [] };
    byID.set(it.id, n);
    const p = it.parent ? byID.get(it.parent) : undefined;
    if (p) p.kids.push(n);
    else roots.push(n);
  }
  return roots;
}

/** matchTests keeps the nodes whose label (or a child's) holds q, any
 * case; "" keeps all. */
export function matchTests(nodes: readonly TestNode[], q: string): TestNode[] {
  const s = q.trim().toLowerCase();
  if (!s) return nodes.slice();
  const out: TestNode[] = [];
  for (const n of nodes) {
    if (n.item.label.toLowerCase().includes(s)) {
      out.push(n);
      continue;
    }
    const kids = matchTests(n.kids, s);
    if (kids.length) out.push({ item: n.item, kids });
  }
  return out;
}

/** caseAt is the test declared on a 1-based line of a file. */
export function caseAt(items: readonly TestItem[], path: string, line: number): TestItem | undefined {
  return items.find((x) => x.kind === "case" && x.path === path && x.line === line);
}

/** duration writes how long a test took: "12 ms", "1.2 s", "2 m 05 s". */
export function duration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)} ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)} s`;
  const m = Math.floor(s / 60);
  return `${m} m ${String(Math.floor(s % 60)).padStart(2, "0")} s`;
}
