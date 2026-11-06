// Editor tabs that are not files (desktop): a database table's rows, a
// query editor, an MCP agent's details. Each tab's name says what it is:
//   "@db:" + conn + "/" + table   a table's rows (both parts URI-encoded)
//   "@sql:" + conn                 a query editor on a connection
//   "@mcp:" + agent                an MCP agent's details
//   "@ctr:" + what + ":" + arg + "|" + label  a container read: inspect, history,
//                                  stats, disk usage, search, tags
// (Diff tabs are lib/difftab.ts.)
import { t } from "../strings";

/** CtrWhat is what a container tab shows; inspect-* names the item's kind. */
export type CtrWhat = "inspect-container" | "inspect-image" | "inspect-network" | "inspect-volume" | "history" | "stats" | "df" | "search" | "tags";
const CTR_WHAT: ReadonlySet<string> = new Set(["inspect-container", "inspect-image", "inspect-network", "inspect-volume", "history", "stats", "df", "search", "tags"]);

export type VTab = { kind: "db"; conn: string; table: string } | { kind: "sql"; conn: string } | { kind: "mcp"; name: string } | { kind: "ctr"; what: CtrWhat; arg: string; label: string };

const enc = encodeURIComponent;

function dec(s: string): string | null {
  try {
    return decodeURIComponent(s);
  } catch {
    return null;
  }
}

/** dbTab names the tab of a table's rows. */
export function dbTab(conn: string, table: string): string {
  return "@db:" + enc(conn) + "/" + enc(table);
}

/** sqlTab names the query editor tab of a connection. */
export function sqlTab(conn: string): string {
  return "@sql:" + enc(conn);
}

/** mcpTab names the tab of an MCP agent's details. */
export function mcpTab(name: string): string {
  return "@mcp:" + enc(name);
}

/** ctrTab names the tab of a container read; label is the tab's name. */
export function ctrTab(what: CtrWhat, arg: string, label = arg): string {
  return "@ctr:" + what + ":" + enc(arg) + "|" + enc(label);
}

/** vtabOf reads a tab's name (null: not one of these). */
export function vtabOf(tab: string): VTab | null {
  if (tab.startsWith("@db:")) {
    const rest = tab.slice(4);
    const i = rest.indexOf("/");
    const conn = i > 0 ? dec(rest.slice(0, i)) : null;
    const table = i > 0 ? dec(rest.slice(i + 1)) : null;
    return conn && table ? { kind: "db", conn, table } : null;
  }
  if (tab.startsWith("@sql:")) {
    const conn = dec(tab.slice(5));
    return conn ? { kind: "sql", conn } : null;
  }
  if (tab.startsWith("@mcp:")) {
    const name = dec(tab.slice(5));
    return name !== null ? { kind: "mcp", name } : null;
  }
  if (tab.startsWith("@ctr:")) {
    // The route may hand the name back decoded: an arg never holds "|".
    const rest = tab.slice(5);
    const i = rest.indexOf(":");
    const what = rest.slice(0, i);
    const body = rest.slice(i + 1);
    const j = body.indexOf("|");
    const a = j < 0 ? null : dec(body.slice(0, j)), l = j < 0 ? null : dec(body.slice(j + 1));
    return CTR_WHAT.has(what) && a !== null && l !== null ? { kind: "ctr", what: what as CtrWhat, arg: a, label: l } : null;
  }
  return null;
}

/** vtabName is the tab's name in the tab bar. */
export function vtabName(v: VTab): string {
  if (v.kind === "db") return v.table;
  if (v.kind === "sql") return t("dbv.query_tab", { conn: v.conn });
  if (v.kind === "ctr") return v.label;
  return v.name || t("mcp.no_name");
}

/** vtabIcon is the codicon of the tab. */
export function vtabIcon(v: VTab): string {
  if (v.kind === "ctr") return v.what === "stats" ? "pulse" : v.what === "history" ? "history" : v.what === "search" || v.what === "tags" ? "search" : v.what === "df" ? "graph" : "json";
  return v.kind === "db" ? "table" : v.kind === "sql" ? "database" : "plug";
}
