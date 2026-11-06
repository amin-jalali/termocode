// Editor tabs that are not files (desktop): a database table's rows, a
// query editor, an MCP agent's details. Each tab's name says what it is:
//   "@db:" + conn + "/" + table   a table's rows (both parts URI-encoded)
//   "@sql:" + conn                 a query editor on a connection
//   "@mcp:" + agent                an MCP agent's details
//   "@ctr:" + what + ":" + arg + "|" + label  a container read: inspect, history,
//                                  stats, disk usage, search, tags
//   "@md:" + path                  a Markdown file's preview
//   "@cmp:" + a + "/" + b          two files side by side (both URI-encoded)
//   "@ddl:" + conn + "/" + table  a table's DDL
//   "@scmp:" + conn + "/" + target a schema compare's script
//   "@dbh:"                        the SQL history
//   "@wf:" + workflow + "/" + run  a workflow run's page (both URI-encoded)
// (Diff tabs are lib/difftab.ts.)
import { t } from "../strings";

/** CtrWhat is what a container tab shows; inspect-* names the item's kind. */
export type CtrWhat = "inspect-container" | "inspect-image" | "inspect-network" | "inspect-volume" | "history" | "stats" | "df" | "search" | "tags";
const CTR_WHAT: ReadonlySet<string> = new Set(["inspect-container", "inspect-image", "inspect-network", "inspect-volume", "history", "stats", "df", "search", "tags"]);

export type VTab =
  | { kind: "db" | "ddl" | "scmp"; conn: string; table: string }
  | { kind: "sql"; conn: string }
  | { kind: "mcp"; name: string }
  | { kind: "ctr"; what: CtrWhat; arg: string; label: string }
  | { kind: "md"; path: string }
  | { kind: "cmp"; a: string; b: string }
  | { kind: "dbh" }
  | { kind: "wf"; workflow: string; run: string };

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

/** ddlTab names a table's DDL tab; schemaCmpTab a schema compare of conn with target. */
export const ddlTab = (conn: string, table: string): string => "@ddl:" + enc(conn) + "/" + enc(table);
export const schemaCmpTab = (conn: string, target: string): string => "@scmp:" + enc(conn) + "/" + enc(target);
export const DBH_TAB = "@dbh:";

/** sqlTab names the query editor tab of a connection. */
export function sqlTab(conn: string): string {
  return "@sql:" + enc(conn);
}

/** mcpTab names the tab of an MCP agent's details. */
export function mcpTab(name: string): string {
  return "@mcp:" + enc(name);
}

/** wfTab names the tab of a workflow run's page. */
export function wfTab(workflow: string, run: string): string {
  return "@wf:" + enc(workflow) + "/" + enc(run);
}

/** ctrTab names the tab of a container read; label is the tab's name. */
export function ctrTab(what: CtrWhat, arg: string, label = arg): string {
  return "@ctr:" + what + ":" + enc(arg) + "|" + enc(label);
}

/** mdTab names the preview tab of a Markdown file. */
export function mdTab(path: string): string {
  return "@md:" + enc(path);
}

/** cmpTab names the tab that compares file a (left) with file b. */
export function cmpTab(a: string, b: string): string {
  return "@cmp:" + enc(a) + "/" + enc(b);
}

/** vtabOf reads a tab's name (null: not one of these). */
export function vtabOf(tab: string): VTab | null {
  const two = /^@(db|ddl|scmp):([^/]+)\/(.+)$/.exec(tab);
  if (two) {
    const conn = dec(two[2]);
    const table = dec(two[3]);
    return conn && table ? { kind: two[1] as "db" | "ddl" | "scmp", conn, table } : null;
  }
  if (tab === DBH_TAB) return { kind: "dbh" };
  if (tab.startsWith("@sql:")) {
    const conn = dec(tab.slice(5));
    return conn ? { kind: "sql", conn } : null;
  }
  if (tab.startsWith("@cmp:")) {
    const rest = tab.slice(5);
    const i = rest.indexOf("/");
    const a = i > 0 ? dec(rest.slice(0, i)) : null;
    const b = i > 0 ? dec(rest.slice(i + 1)) : null;
    return a && b ? { kind: "cmp", a, b } : null;
  }
  if (tab.startsWith("@md:")) {
    const path = dec(tab.slice(4));
    return path ? { kind: "md", path } : null;
  }
  if (tab.startsWith("@wf:")) {
    const rest = tab.slice(4);
    const i = rest.indexOf("/");
    const workflow = i > 0 ? dec(rest.slice(0, i)) : null;
    const run = i > 0 ? dec(rest.slice(i + 1)) : null;
    return workflow && run ? { kind: "wf", workflow, run } : null;
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
  if (v.kind === "ddl") return t("dbv.ddl_tab", { table: v.table });
  if (v.kind === "scmp") return v.table + " ↔ " + v.conn;
  if (v.kind === "dbh") return t("dbv.history");
  if (v.kind === "sql") return t("dbv.query_tab", { conn: v.conn });
  if (v.kind === "wf") return t("wfv.tab", { name: v.workflow });
  if (v.kind === "ctr") return v.label;
  if (v.kind === "cmp") return v.a.slice(v.a.lastIndexOf("/") + 1) + " ↔ " + v.b.slice(v.b.lastIndexOf("/") + 1);
  if (v.kind === "md") return t("md.preview_tab", { name: v.path.slice(v.path.lastIndexOf("/") + 1) });
  return v.name || t("mcp.no_name");
}

/** vtabIcon is the codicon of the tab. */
export function vtabIcon(v: VTab): string {
  if (v.kind === "ctr") return v.what === "stats" ? "pulse" : v.what === "history" ? "history" : v.what === "search" || v.what === "tags" ? "search" : v.what === "df" ? "graph" : "json";
  return v.kind === "db" ? "table" : v.kind === "sql" ? "database" : v.kind === "ddl" ? "code" : v.kind === "scmp" ? "diff" : v.kind === "dbh" ? "history" : v.kind === "md" ? "open-preview" : v.kind === "cmp" ? "compare-changes" : v.kind === "wf" ? "type-hierarchy" : "plug";
}
