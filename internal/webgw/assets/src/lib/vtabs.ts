// Editor tabs that are not files (desktop): a database table's rows, a
// query editor, an MCP agent's details. Each tab's name says what it is:
//   "@db:" + conn + "/" + table   a table's rows (both parts URI-encoded)
//   "@sql:" + conn                 a query editor on a connection
//   "@mcp:" + agent                an MCP agent's details
//   "@ddl:" + conn + "/" + table  a table's DDL
//   "@cmp:" + conn + "/" + target a schema compare's script
//   "@dbh:"                        the SQL history
// (Diff tabs are lib/difftab.ts.)
import { t } from "../strings";

export type VTab =
  | { kind: "db" | "ddl" | "cmp"; conn: string; table: string }
  | { kind: "sql"; conn: string }
  | { kind: "mcp"; name: string }
  | { kind: "dbh" };

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

/** ddlTab names a table's DDL tab; cmpTab a schema compare of conn with target. */
export const ddlTab = (conn: string, table: string): string => "@ddl:" + enc(conn) + "/" + enc(table);
export const cmpTab = (conn: string, target: string): string => "@cmp:" + enc(conn) + "/" + enc(target);
export const DBH_TAB = "@dbh:";

/** sqlTab names the query editor tab of a connection. */
export function sqlTab(conn: string): string {
  return "@sql:" + enc(conn);
}

/** mcpTab names the tab of an MCP agent's details. */
export function mcpTab(name: string): string {
  return "@mcp:" + enc(name);
}

/** vtabOf reads a tab's name (null: not one of these). */
export function vtabOf(tab: string): VTab | null {
  const two = /^@(db|ddl|cmp):([^/]+)\/(.+)$/.exec(tab);
  if (two) {
    const conn = dec(two[2]);
    const table = dec(two[3]);
    return conn && table ? { kind: two[1] as "db" | "ddl" | "cmp", conn, table } : null;
  }
  if (tab === DBH_TAB) return { kind: "dbh" };
  if (tab.startsWith("@sql:")) {
    const conn = dec(tab.slice(5));
    return conn ? { kind: "sql", conn } : null;
  }
  if (tab.startsWith("@mcp:")) {
    const name = dec(tab.slice(5));
    return name !== null ? { kind: "mcp", name } : null;
  }
  return null;
}

/** vtabName is the tab's name in the tab bar. */
export function vtabName(v: VTab): string {
  if (v.kind === "db") return v.table;
  if (v.kind === "ddl") return t("dbv.ddl_tab", { table: v.table });
  if (v.kind === "cmp") return v.table + " ↔ " + v.conn;
  if (v.kind === "dbh") return t("dbv.history");
  if (v.kind === "sql") return t("dbv.query_tab", { conn: v.conn });
  return v.kind === "mcp" ? v.name || t("mcp.no_name") : "";
}

/** vtabIcon is the codicon of the tab. */
export function vtabIcon(v: VTab): string {
  return v.kind === "db" ? "table" : v.kind === "sql" ? "database" : v.kind === "ddl" ? "code" : v.kind === "cmp" ? "diff" : v.kind === "dbh" ? "history" : "plug";
}
