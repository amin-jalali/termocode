// A grid's rows as text (Export Result…, in aictx.js beside Add to AI
// Chat: both turn the rows the page shows into text for somewhere else,
// and both load on first use). Pure.
import type { DBCellView, DBRowsView } from "../state/more";

export type Fmt = "csv" | "json" | "md";

const cellText = (c: DBCellView | undefined) => (!c || c.n ? "" : c.m ? "••••••" : c.v);

/** exportText writes rows as CSV (a formula-like cell gets a ' first),
 * JSON (NULL as null) or a Markdown table. */
export function exportText(r: DBRowsView, f: Fmt): string {
  if (f === "json")
    return JSON.stringify(r.rows.map((row) => Object.fromEntries(r.columns.map((c, j) => [c, row[j]?.n ? null : cellText(row[j])]))), null, 2) + "\n";
  if (f === "md") {
    const esc = (s: string) => s.replace(/\|/g, "\\|").replace(/\n/g, " ");
    const line = (cells: string[]) => "| " + cells.map(esc).join(" | ") + " |";
    return [line(r.columns), "|" + r.columns.map(() => " --- |").join(""), ...r.rows.map((row) => line(r.columns.map((_, j) => (row[j]?.n ? "NULL" : cellText(row[j])))))].join("\n") + "\n";
  }
  const q = (s: string) => {
    if (/^[=+\-@\t\r]/.test(s)) s = "'" + s;
    return /[",\n\r]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s;
  };
  return [r.columns.map(q).join(","), ...r.rows.map((row) => r.columns.map((_, j) => q(cellText(row[j]))).join(","))].join("\r\n") + "\r\n";
}
