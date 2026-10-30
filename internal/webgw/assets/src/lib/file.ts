// The file the file view shows (WEB-18): buf.open loads it into the
// laptop's editor (hidden, the laptop's window does not change), then the
// page follows buf:<n>. A file that may hold secrets, a binary file or a
// file too big to open is never sent: the view shows what it is instead
// (a big file shows its first 2 000 lines read-only, from buf.head).
// Leaving the view unsubscribes and drops the page's own reference with
// buf.close; the laptop's buffer stays.
import { signal } from "@preact/signals";
import type { BufHeadRes, BufOpenRes } from "../../../../webproto/proto";
import { OfflineError, ReqError } from "../net/link";
import { link } from "../session";

export type FileKind = "loading" | "open" | "head" | "binary" | "secret" | "missing" | "offline" | "error";

export interface FileState {
  path: string;
  kind: FileKind;
  /** buf is the editor buffer (kind open). */
  buf?: number;
  /** head is the start of a file too big to open (kind head). */
  head?: BufHeadRes;
  /** size in bytes, when the hub said (big and binary files). */
  size?: number;
  /** err is the refusal (kind error). */
  err?: unknown;
}

export const file = signal<FileState | null>(null);

let gen = 0;

function reason(e: unknown): { reason?: string; size?: number } {
  const d = e instanceof ReqError && typeof e.data === "object" && e.data !== null ? (e.data as Record<string, unknown>) : {};
  return { reason: typeof d.reason === "string" ? d.reason : undefined, size: typeof d.size === "number" ? d.size : undefined };
}

function set(g: number, st: FileState): void {
  if (g === gen) file.value = st;
}

/** kindOf maps a refusal of buf.open or buf.head to what the view shows. */
function kindOf(e: unknown): FileKind {
  if (e instanceof OfflineError) return "offline";
  if (!(e instanceof ReqError)) return "error";
  if (e.code === "forbidden") return "secret";
  if (e.code === "not_found") return "missing";
  if (e.code === "limit" && reason(e).reason === "binary") return "binary";
  return "error";
}

function head(g: number, path: string, size?: number): void {
  link.request("buf.head", { path }).then(
    (data) => set(g, { path, kind: "head", head: data as BufHeadRes, size: (data as BufHeadRes).size ?? size }),
    (e: unknown) => set(g, { path, kind: kindOf(e), size: reason(e).size ?? size, err: e }),
  );
}

/** openPath shows path: buf.open, then the buf topic. */
export function openPath(path: string): void {
  const g = ++gen;
  closeCurrent();
  file.value = { path, kind: "loading" };
  link.request("buf.open", { path }).then(
    (data) => {
      const res = data as BufOpenRes;
      if (g !== gen) {
        link.request("buf.close", { buf: res.buf }).catch(() => undefined);
        return;
      }
      link.subscribe("buf:" + res.buf);
      set(g, { path, kind: "open", buf: res.buf });
    },
    (e: unknown) => {
      const r = reason(e);
      if (e instanceof ReqError && e.code === "limit" && r.reason === "big") {
        head(g, path, r.size);
        return;
      }
      set(g, { path, kind: kindOf(e), size: r.size, err: e });
    },
  );
}

function closeCurrent(): void {
  const cur = file.peek();
  if (cur?.buf === undefined) return;
  link.unsubscribe("buf:" + cur.buf);
  link.request("buf.close", { buf: cur.buf }).catch(() => undefined); // only the page's own reference
}

/** closePath leaves the file view of path. */
export function closePath(path: string): void {
  if (file.peek()?.path !== path) return;
  gen++;
  closeCurrent();
  file.value = null;
}
