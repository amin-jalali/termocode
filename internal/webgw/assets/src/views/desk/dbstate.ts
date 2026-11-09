// The desktop Databases view's shared state: the connections (with the
// laptop session's state), the open form in the view, the query text of
// each connection's query tab and the result pinned in it. Desktop only.
import { signal } from "@preact/signals";
import { ReqError } from "../../net/link";
import { link } from "../../session";
import { readDBConns, type DBConnView, type readDBTables } from "../../state/more";
import type { RedisKeys } from "../../../../../webproto/proto";
import { dbErr } from "../../ui/DBAsk";

export type Load<T> = { s: "loading" } | { s: "ok"; v: T } | { s: "err"; msg: string; need?: DBNeed };

/** DBNeed is what a failed database read asks the user for (the
 * laptop's err data, webproto.DBNeed): an unknown SSH host key to check
 * and trust, or a secret the laptop does not have (key: the SSH key
 * file's name; wrong: the one it had was not accepted). */
export type DBNeed =
  | { need: "host_key"; host: string; key_type?: string; fingerprint: string; jump?: boolean }
  | { need: DBSecretKind; key?: string; wrong?: boolean };

/** DBSecretKind is a secret db.secret takes. */
export type DBSecretKind = "password" | "ssh_password" | "ssh_passphrase";

/** dbNeed is the DBNeed of a failed request (undefined: none). */
export function dbNeed(e: unknown): DBNeed | undefined {
  if (!(e instanceof ReqError) || e.code !== "conflict") return undefined;
  const d = e.data as { need?: unknown; host?: unknown; fingerprint?: unknown } | null | undefined;
  if (d?.need === "host_key") return typeof d.host === "string" && typeof d.fingerprint === "string" ? (d as DBNeed) : undefined;
  if (d?.need === "password" || d?.need === "ssh_password" || d?.need === "ssh_passphrase") return d as DBNeed;
  return undefined;
}

export const conns = signal<Load<DBConnView[]> | null>(null);

/** tables is the tree's read children of each connection (its tables,
 * or a Redis connection's keys), by connection id. */
export const tables = signal<ReadonlyMap<string, Load<ReturnType<typeof readDBTables> | RedisKeys>>>(new Map());

/** loadConns asks for the connections again. */
export function loadConns(): void {
  if (!conns.peek()) conns.value = { s: "loading" };
  link.request("db.conns", {}).then(
    (r) => (conns.value = { s: "ok", v: readDBConns(r) }),
    (e: unknown) => (conns.value = { s: "err", msg: dbErr(e) }),
  );
}

export { dbErr };

/** connView is a listed connection by id. */
export function connView(id: string): DBConnView | undefined {
  const c = conns.peek();
  return c?.s === "ok" ? c.v.find((x) => x.id === id) : undefined;
}

/** sqlConns are the listed connections the page can query. */
export function sqlConns(): DBConnView[] {
  const c = conns.peek();
  return c?.s === "ok" ? c.v.filter((x) => x.sql) : [];
}

/** DBForm is the form open in the view. */
export type DBForm =
  | { kind: "add" | "pick-console" | "pick-file" }
  | { kind: "ssh" | "ssl" | "compare" | "tx"; conn: string }
  | { kind: "import"; conn: string; table: string }
  | { kind: "ttl"; conn: string; db: number; key: string };

export const dbForm = signal<DBForm | null>(null);

/** filter narrows the tree (null: no filter box). */
export const dbFilter = signal<string | null>(null);

/** queries keeps each connection's query text while its tab is closed;
 * runNext asks a query tab to run its text once it opens (Run File). */
export const queries = signal<ReadonlyMap<string, string>>(new Map());
export const runNext = signal<string | null>(null);

export function setQuery(conn: string, sql: string): void {
  const n = new Map(queries.peek());
  n.set(conn, sql);
  queries.value = n;
}

/** pushed is a text set from outside (History, Run File) for an open tab. */
export const pushed = signal<{ conn: string; sql: string } | null>(null);

export function pushQuery(conn: string, sql: string): void {
  setQuery(conn, sql);
  pushed.value = { conn, sql };
}

export const canAct = (): boolean => link.meta.peek()?.role !== "viewer";
