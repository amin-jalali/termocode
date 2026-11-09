// Add to AI Chat (aictx.js, desktop web): an item of the page goes to
// the AI chat as context, with no typing. It waits as a chip above the
// chat input (the composer's attach chips) and goes with the next
// message as an item attach (webproto ItemSpec): its exact address in
// the names termocode's MCP tools take, and a small summary (a table's
// DDL, the picked rows, at most 50). Types and pure helpers only are
// imported; the workbench gives the rest (api.ts).
import type { Attach } from "../../../../webproto/proto";
import { readChat } from "../state/chat";
import type { AIHost, AIItem, GridAt, ItemSpec } from "./api";
import { H, setHost } from "./jsx";
import { askSide } from "./side";

export { exportText } from "./export";
/** side is AI: Side Question… (side.tsx). */
export { askSide as side };

/** init takes the workbench's host (once, on the first load). */
export function init(host: AIHost): void {
  setHost(host);
}

/** MAX_ROWS is how many rows one item carries (webproto MaxItemRows). */
const MAX_ROWS = 50;
const MAX_DDL = 16 << 10;
/** MAX_SPEC keeps a spec under webproto MaxItemSpec (32 KiB). */
const MAX_SPEC = 30 << 10;

/** got is a load's value (undefined: not read). */
const got = <T,>(l: { s: string; v?: T } | null | undefined): T | undefined => (l?.s === "ok" ? l.v : undefined);

const connName = (id: string): string => (got(H.conns.peek()) ?? []).find((c) => c.id === id)?.name || id;

/** schemaOf is the schema the tree read for conn ("" when none). */
function schemaOf(conn: string): string {
  const v = got(H.dbTables.peek().get(conn));
  return v && "schema" in v ? v.schema : "";
}

/** label is the chip's text: the item's path, "local › users › email". */
export function label(it: AIItem): string {
  const db = it.conn ? [connName(it.conn), it.table] : [it.host, it.project];
  const n = it.rows?.length ?? 0;
  const end =
    it.item === "db_cell"
      ? ["row " + it.row, it.column]
      : it.item === "db_rows"
        ? [n === 1 ? "row " + it.row : n + " rows"]
        : [it.item === "container" ? it.container : it.column];
  return [...db, ...end].filter(Boolean).join(" › ");
}

/** address is what names the item (its spec without the summary). */
function address(s: ItemSpec): string {
  return JSON.stringify([s.item, s.host, s.project, s.container, s.conn, s.schema, s.table, s.column, s.rows]);
}

/** add puts the item into the chat's next message. */
export async function add(it: AIItem): Promise<void> {
  const { row, ...spec } = it;
  if (spec.conn && spec.table) {
    const sc = schemaOf(spec.conn);
    if (sc) spec.schema = sc;
  }
  if (spec.item === "db_table") {
    try {
      const r = (await H.link.request("db.ddl", { conn: spec.conn, table: spec.table })) as { text?: string };
      if (r.text) spec.ddl = r.text.slice(0, MAX_DDL);
    } catch {
      // The chip goes without the DDL: the AI reads it with db_schema.
    }
  }
  while (spec.rows && spec.rows.length > 1 && JSON.stringify(spec).length > MAX_SPEC) spec.rows = spec.rows.slice(0, -1);
  const chat = H.pinned.peek()?.id ?? readChat(H.store.topic("chat").peek()?.data)?.id ?? "";
  const key = address(spec);
  const name = label({ ...spec, row });
  if ((H.attaches.peek()[chat] ?? []).some((a) => a.kind === "item" && same(a, key))) return void H.showChat();
  const a: Attach = { kind: "item", label: name.slice(0, 120), spec: JSON.stringify(spec) };
  if (!H.addAttach(chat, a)) return void H.toast("warn", H.t("ai.ctx_full"));
  H.showChat();
  H.toast("ok", H.t("ai.attached"));
}

function same(a: Attach, key: string): boolean {
  try {
    return address(JSON.parse(a.spec ?? "") as ItemSpec) === key;
  } catch {
    return false;
  }
}

/** cellOf is a grid cell's value as the item carries it (null: NULL). */
const cellOf = (c: { v: string; n: boolean; m: boolean }): string | null => (c.n ? null : c.m ? "•••" : c.v);

/** grid opens the menu of a right click in a table's grid: the column
 * (its header), the cell, the row (its number) or the shown rows (the
 * corner). */
export function grid(g: GridAt): void {
  const { r, conn, table } = g;
  const cell = g.el.closest("td,th") as HTMLTableCellElement | null;
  const tr = cell?.closest("tr");
  if (!cell || !tr) return;
  const col = cell.cellIndex - 1;
  const row = tr.dataset.row ? +tr.dataset.row : -1;
  const base: AIItem = { item: "db_column", conn, table };
  let it: AIItem;
  if (row < 0 && col >= 0) it = { ...base, column: r.columns[col] };
  else {
    const rows = row < 0 ? r.rows.slice(0, MAX_ROWS) : [r.rows[row]];
    if (!rows.length || !rows[0]) return;
    it = { ...base, item: col < 0 ? "db_rows" : "db_cell", columns: r.columns, rows: rows.map((x) => x.map(cellOf)), row: g.from + Math.max(row, 0) + 1 };
    if (r.key.length) it.key = r.key;
    // Values the laptop masked already (••••) go masked.
    const hid = r.columns.filter((_, j) => rows.some((x) => x[j]?.m));
    if (hid.length) it.masked = hid;
    if (col >= 0) it.column = r.columns[col];
  }
  H.openMenu(g.x, g.y, label(it), [{ label: H.t("ai.attach"), id: "ai-add", run: () => void add(it) }]);
}
