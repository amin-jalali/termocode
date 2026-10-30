// File transfer over the link (WEB-49): bytes go base64 in chunks inside
// the JSON frames, one request each. Pure helpers here, plus the two
// loops; the sheets in views/files/Transfer.tsx show their progress.
import { link } from "../session";

/** MAX_TRANSFER is webproto.MaxTransfer: 256 MB up or down. */
export const MAX_TRANSFER = 256 << 20;

/** toBase64 encodes bytes (in pieces, so a big chunk does not overflow the stack). */
export function toBase64(b: Uint8Array): string {
  let s = "";
  for (let i = 0; i < b.length; i += 0x8000) s += String.fromCharCode(...b.subarray(i, i + 0x8000));
  return btoa(s);
}

/** fromBase64 decodes what toBase64 made. */
export function fromBase64(s: string): Uint8Array<ArrayBuffer> {
  const bin = atob(s);
  const out = new Uint8Array(new ArrayBuffer(bin.length));
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export class TransferStopped extends Error {}

type Obj = Record<string, unknown>;
const obj = (v: unknown): Obj => (typeof v === "object" && v !== null ? (v as Obj) : {});
const n = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) ? v : 0);

/** download reads a file chunk by chunk; progress gets bytes done of all.
 * A file that changes between chunks fails; stop() ends it early. */
export async function download(path: string, progress: (done: number, all: number) => void, stopped: () => boolean): Promise<Blob> {
  const parts: Uint8Array<ArrayBuffer>[] = [];
  let from = 0;
  let mtime = -1;
  for (;;) {
    if (stopped()) throw new TransferStopped();
    const c = obj(await link.request("file.download", from ? { path, from } : { path }));
    if (mtime >= 0 && n(c.mtime) !== mtime) throw new Error("changed");
    mtime = n(c.mtime);
    const bytes = fromBase64(typeof c.data === "string" ? c.data : "");
    parts.push(bytes);
    from += bytes.length;
    progress(from, n(c.size));
    if (!n(c.next)) break;
    from = n(c.next);
  }
  return new Blob(parts);
}

/** saveBlob hands the bytes to the browser's download, under name. The
 * link is made outside the page's tree and gone at once. */
export function saveBlob(b: Blob, name: string): void {
  const url = URL.createObjectURL(b);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.rel = "noopener";
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export interface Upload {
  id: string;
  exists: boolean;
}

/** upload sends a file to path; it returns the upload, staged on the laptop. */
export async function upload(path: string, f: Blob, progress: (done: number, all: number) => void, stopped: () => boolean): Promise<Upload> {
  const st = obj(await link.request("file.upload_begin", { path, size: f.size }));
  const id = typeof st.id === "string" ? st.id : "";
  const chunk = n(st.chunk) || 512 << 10;
  try {
    for (let off = 0; off < f.size; off += chunk) {
      if (stopped()) throw new TransferStopped();
      const bytes = new Uint8Array(await f.slice(off, off + chunk).arrayBuffer());
      await link.request("file.upload_chunk", { id, off, data: toBase64(bytes) });
      progress(Math.min(off + chunk, f.size), f.size);
    }
  } catch (e) {
    link.request("file.upload_cancel", { id }).catch(() => undefined);
    throw e;
  }
  return { id, exists: st.exists === true };
}
