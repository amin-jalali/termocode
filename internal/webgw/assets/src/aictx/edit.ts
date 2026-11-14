// A table's pending edits as db.edit's args (in aictx.js beside the
// grid's Export menu: it runs only when the user saves, so the
// workbench's first file does not carry it). Pure.
import type { DBCellView, DBRowsView } from "../state/more";
import type { Edit } from "../views/desk/DBTabs";

/** editRequest is db.edit's args for the pending changes. */
export function editRequest(conn: string, table: string, r: DBRowsView, e: Edit): unknown {
  const val = (col: string, c: DBCellView) => (c.n ? { col, n: true } : { col, v: c.v });
  const keyOf = (row: DBCellView[]) => r.key.map((k) => val(k, row[r.columns.indexOf(k)]));
  const edits: unknown[] = [];
  e.del.forEach((i) => edits.push({ kind: "delete", key: keyOf(r.rows[i]) }));
  r.rows.forEach((row, i) => {
    if (e.del.has(i)) return;
    const set: unknown[] = [];
    const old: unknown[] = [];
    r.columns.forEach((col, j) => {
      const c = e.set.get(i + ":" + j);
      if (!c) return;
      set.push(val(col, c));
      if (!row[j].m && !row[j].c) old.push(val(col, row[j]));
    });
    if (set.length) edits.push({ kind: "update", key: keyOf(row), set, old: old.length ? old : undefined });
  });
  e.add.forEach((row, a) => {
    const set: unknown[] = [];
    r.columns.forEach((col, j) => {
      const c = e.set.get(r.rows.length + a + ":" + j) ?? row[j];
      if (c) set.push(val(col, c));
    });
    if (set.length) edits.push({ kind: "insert", set });
  });
  return { conn, table, edits };
}
