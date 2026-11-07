// The words of a spec chip (MOBUX-17): one line that says what a view
// spec built, "table · Status, Total · Status = paid · Total ↓". The
// laptop writes the same line for the AI (internal/app
// ai_view_show_tool.go, aiViewSpecSummary); keep both in step. Field
// ids become their labels from the block's columns when it has them.
import type { Block, ViewSpec } from "../../../../webproto/proto";

const OPS: Record<string, string> = {
  eq: "=",
  ne: "≠",
  gt: ">",
  lt: "<",
  ge: "≥",
  le: "≤",
  contains: "has",
  starts: "starts with",
  in: "in",
  empty: "is empty",
  not_empty: "is set",
};

/** labelsOf maps a block's column ids to their labels. */
export function labelsOf(b: Block): Map<string, string> {
  const m = new Map<string, string>();
  for (const c of b.columns ?? []) m.set(c.id, c.label);
  return m;
}

/** specSummary is the chip line of s; label names a field id. */
export function specSummary(s: ViewSpec, labels: Map<string, string> = new Map()): string {
  const name = (id: string) => labels.get(id) || id;
  const parts: string[] = [];
  if (s.layout) parts.push(s.layout);
  if (s.fields && s.fields.length > 0) parts.push(s.fields.map(name).join(", "));
  for (const c of s.filter ?? []) {
    let p = `${name(c.field)} ${OPS[c.op] ?? c.op}`;
    if (c.op === "in") p += " " + (c.in ?? []).join("|");
    else if (c.op !== "empty" && c.op !== "not_empty") p += " " + (c.value ?? "");
    parts.push(p);
  }
  for (const o of s.sort ?? []) parts.push(`${name(o.field)} ${o.desc ? "↓" : "↑"}`);
  if (s.group) parts.push("by " + name(s.group));
  if (s.limit && s.limit > 0) parts.push(`${s.limit} rows`);
  if (s.density === "compact") parts.push("compact");
  return parts.join(" · ");
}
