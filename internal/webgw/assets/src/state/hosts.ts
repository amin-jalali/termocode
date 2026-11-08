// The hosts tree of the Containers and Databases views, as the laptop
// draws it: every engine host at once ("name · where"), each one's
// containers read once when it is first opened (Refresh reads them
// again), grouped by compose project. A host that does not answer keeps
// its one-line reason under its own row; the others stay. One host list
// and one cache for both views: the desktop Containers view fills it
// from containers.host (CONTAINERS is the hosts tree), the phone and the
// Databases view ask containers.servers when nothing filled it yet.
// Phone and desktop share this state.
import { signal } from "@preact/signals";
import { OfflineError, ReqError } from "../net/link";
import { link } from "../session";
import { t } from "../strings";

/** HostView is one host (a context). where is user@host ("" and local:
 * this machine); ssh marks an ssh:// one. */
export interface HostView {
  name: string;
  where: string;
  local: boolean;
  ssh: boolean;
  current: boolean;
  session: boolean;
}

/** HostCtr is one container of a host (db: its database driver). */
export interface HostCtr {
  id: string;
  name: string;
  image: string;
  state: string;
  status: string;
  project: string;
  db: string;
}

export type HostLoad<T> = { s: "loading" } | { s: "ok"; v: T } | { s: "err"; msg: string };

const obj = (v: unknown): Record<string, unknown> => (typeof v === "object" && v !== null ? (v as Record<string, unknown>) : {});
const str = (v: unknown): string => (typeof v === "string" ? v : "");
const arr = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);

/** readHosts reads hosts: containers.servers' hosts or containers.host's contexts. */
export function readHosts(list: unknown): HostView[] {
  return arr(list)
    .map(obj)
    .filter((h) => str(h.name) !== "")
    .map((h) => ({ name: str(h.name), where: str(h.where), local: h.local === true, ssh: h.ssh === true, current: h.current === true, session: h.session === true }));
}

function readCtrs(data: unknown): HostCtr[] {
  return arr(obj(data).containers)
    .map(obj)
    .filter((c) => /^[0-9a-f]{12,64}$/.test(str(c.id)))
    .map((c) => ({ id: str(c.id), name: str(c.name), image: str(c.image), state: str(c.state), status: str(c.status), project: str(c.project), db: str(c.db) }));
}

/** inUse is the host the laptop's engine talks to now: the session's,
 * else the default (null: not known). */
export const inUse = (list: HostView[]): HostView | null => list.find((x) => x.session) ?? list.find((x) => x.current) ?? null;

/** upOf is "running/all" of a host's containers. */
export const upOf = (cs: readonly { state: string }[]): string => cs.filter((c) => c.state === "running").length + "/" + cs.length;

/** hostLabel is "name · where" (this machine for a local one). */
export const hostLabel = (h: HostView): string => h.name + " · " + (h.local || !h.where ? t("srv.this_machine") : h.where);

/** errLine is the line of a failed host request. */
export function errLine(e: unknown): string {
  if (e instanceof OfflineError) return t("offline.input");
  if (e instanceof ReqError && e.message && e.code !== "internal") return e.message;
  return t("err.internal");
}

/** hosts is the one host list (null: not read yet). */
export const hosts = signal<HostLoad<HostView[]> | null>(null);
/** hostCtrs is each read host's containers, by host name. */
export const hostCtrs = signal<ReadonlyMap<string, HostLoad<HostCtr[]>>>(new Map());

/** setHosts takes the Containers view's contexts as the host list. */
export function setHosts(list: HostView[]): void {
  hosts.value = { s: "ok", v: list };
}

/** loadHosts asks for the host list (containers.servers). */
export function loadHosts(): void {
  if (!hosts.peek()) hosts.value = { s: "loading" };
  link.request("containers.servers", {}).then(
    (r) => (hosts.value = { s: "ok", v: readHosts(obj(r).hosts) }),
    (e: unknown) => (hosts.value = { s: "err", msg: errLine(e) }),
  );
}

function setCtrs(name: string, l: HostLoad<HostCtr[]>): void {
  const n = new Map(hostCtrs.peek());
  n.set(name, l);
  hostCtrs.value = n;
}

/** loadHost reads one host's containers (once per open, or Refresh). */
export function loadHost(name: string): void {
  setCtrs(name, { s: "loading" });
  link.request("containers.server", { name }).then(
    (r) => setCtrs(name, { s: "ok", v: readCtrs(r) }),
    (e: unknown) => setCtrs(name, { s: "err", msg: errLine(e) }),
  );
}

/** openHost reads a host when the page has nothing (or a failure) for it. */
export function openHost(name: string): void {
  const l = hostCtrs.peek().get(name);
  if (!l || l.s === "err") loadHost(name);
}

/** forgetHosts drops every read host (Refresh reads the open ones again). */
export function forgetHosts(open: Iterable<string>): void {
  hostCtrs.value = new Map();
  for (const name of open) loadHost(name);
}

/** groups are a host's containers by compose project (other: none). */
export function groups(cs: HostCtr[]): [string, HostCtr[]][] {
  const by = new Map<string, HostCtr[]>();
  for (const c of cs) {
    const p = c.project || t("srv.other");
    by.set(p, [...(by.get(p) ?? []), c]);
  }
  return [...by].sort(([a], [b]) => a.localeCompare(b));
}

/** NAME and SSH_HOST are what + Add Server… takes (as the hub checks). */
export const NAME = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
export const SSH_HOST = /^ssh:\/\/[A-Za-z0-9._][A-Za-z0-9._-]*@[A-Za-z0-9.[\]:_-]+$/;

/** addServer creates a context for ssh://user@host. */
export function addServer(name: string, host: string): Promise<unknown> {
  return link.request("containers.add_server", { name, host });
}

/** pickDB adds a host's database container to Databases; it answers
 * the connection's id. */
export function pickDB(host: string, id: string): Promise<{ conn: string; name: string; existing: boolean }> {
  return link.request("db.add_container", { host, id }).then((r) => {
    const o = obj(r);
    return { conn: str(o.conn), name: str(o.name), existing: o.existing === true };
  });
}

/** openNext is a connection the Databases view opens once it lists it
 * (a database container just added from a host). */
export const openNext = signal<string | null>(null);
