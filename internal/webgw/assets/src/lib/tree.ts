// The Files tree's own state (WEB-16): which folders are open on this
// page, which listings failed, and the dotfile filter. The listings
// themselves live in the tree topic; this module asks the hub to list a
// folder (tree.list) when it is opened and to stop (tree.unlist) when it
// is closed, and lists them again after a new connection.
import { signal } from "@preact/signals";
import { OfflineError, ReqError } from "../net/link";
import { link } from "../session";
import { has } from "../state/dict";
import { under } from "../state/files";
import { t } from "../strings";
import { toast } from "./toasts";

/** expanded holds the open folders (the root "" is always listed). */
export const expanded = signal<ReadonlySet<string>>(new Set());
/** failed holds the folders whose listing failed, with the reason. */
export const failed = signal<ReadonlyMap<string, string>>(new Map());
/** hideDots hides dotfiles here even when the laptop shows them. */
export const hideDots = signal(false);

/** MAX_TRIES caps the listings of one folder per connection, so a hub
 * that never publishes a folder cannot make the page ask forever. */
const MAX_TRIES = 3;

const pending = new Set<string>();
const asked = new Set<string>(); // listed on this connection, the listing not in the topic yet
const seen = new Set<string>(); // folders the topic had on this connection
const tries = new Map<string, number>();
let conn = "";

function setFailed(path: string, why: string | null): void {
  const next = new Map(failed.peek());
  if (why === null) next.delete(path);
  else next.set(path, why);
  failed.value = next;
}

/** list asks the hub for one folder; the listing arrives in the topic. */
export function list(path: string): void {
  if (pending.has(path) || !link.live.peek()) return;
  const n = tries.get(path) ?? 0;
  if (n >= MAX_TRIES) return;
  tries.set(path, n + 1);
  pending.add(path);
  link.request("tree.list", { path }).then(
    () => {
      pending.delete(path);
      asked.add(path);
    },
    (e: unknown) => {
      pending.delete(path);
      if (e instanceof OfflineError) return;
      if (e instanceof ReqError && e.code === "limit") {
        toast("warn", t("files.too_many"));
        collapse(path);
        return;
      }
      if (e instanceof ReqError && e.code === "not_found" && path !== "") {
        collapse(path); // the folder is gone
        return;
      }
      setFailed(path, e instanceof Error ? e.message : "");
    },
  );
}

/** retry lists a failed folder again. */
export function retry(path: string): void {
  setFailed(path, null);
  tries.delete(path);
  list(path);
}

/** collapse closes a folder and every open folder under it. */
export function collapse(path: string): void {
  if (path === "") return; // the root stays listed
  const gone = [...expanded.peek()].filter((p) => under(path, p));
  expanded.value = new Set([...expanded.peek()].filter((p) => !under(path, p)));
  for (const p of gone) {
    const listed = seen.has(p) || asked.has(p) || pending.has(p);
    seen.delete(p);
    asked.delete(p);
    tries.delete(p);
    if (failed.peek().has(p)) setFailed(p, null);
    // The hub lists a folder for this page only after a good tree.list.
    if (listed) link.request("tree.unlist", { path: p }).catch(() => undefined); // advisory
  }
}

/** toggle opens or closes a folder. */
export function toggle(path: string): void {
  if (expanded.peek().has(path)) {
    collapse(path);
    return;
  }
  expanded.value = new Set([...expanded.peek(), path]);
  list(path);
}

/** reconcile lists every open folder the topic does not have yet: at the
 * start, after a new connection (the hub forgot this page's folders) and
 * after a remove delta for a folder that still exists. A folder the topic
 * had and then dropped while this page still lists it is closed. stale
 * data (before the fresh snap of a new connection) is not judged. */
export function reconcile(dirs: Readonly<Record<string, unknown>>, stale: boolean): void {
  if (!link.live.peek() || stale) return;
  const now = link.meta.peek()?.conn ?? "";
  if (now !== conn) {
    conn = now;
    seen.clear();
    asked.clear();
    tries.clear();
  }
  for (const p of ["", ...expanded.peek()]) {
    if (has(dirs, p)) {
      seen.add(p);
      asked.delete(p);
      continue;
    }
    if (seen.has(p)) {
      seen.delete(p);
      if (p !== "") {
        collapse(p);
        continue;
      }
    }
    if (!asked.has(p) && !failed.peek().has(p)) list(p);
  }
}
