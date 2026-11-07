// "↩ to chat" (MOBUX-19, mobile plan 3.3): any row or block on the phone
// can go back into the chat. It goes as a reference (kind, id, path,
// line), never as its contents: the laptop resolves it under the same
// secret rules as every op (MOB-5). The page refuses a secret-looking
// file at once, so the user hears "stays on the laptop" before Send.
// The refs wait above the composer as chips; at most MAX_REFS go with one
// message. Nothing here is stored on the device.
import { signal } from "@preact/signals";
import { looksSecret } from "./secretish";

/** ChatRef is a reference to one thing on the laptop: webproto BlockRef. */
export interface ChatRef {
  kind: string;
  id?: string;
  path?: string;
  line?: number;
}

/** RefChip is a ref waiting in the composer, with the label it shows. The
 * label stays on the page; only the ref is sent. */
export interface RefChip {
  key: string;
  ref: ChatRef;
  label: string;
}

/** The attach kinds of the plan (3.3). */
const KINDS = new Set(["task", "ptask", "issue", "pr", "file", "lines", "diff", "worktree", "run", "race", "workflow", "card", "resource", "block", "project", "view"]);

export const MAX_REFS = 8;
const MAX_PATH = 512; // blocks.MaxPath
const MAX_LABEL_SENT = 120; // webproto.MaxAttachLabel

// idMax is the longest id a kind takes (webproto MaxID, MaxLabel,
// MaxAttachURI).
function idMax(kind: string): number {
  return kind === "resource" ? 1024 : kind === "task" || kind === "ptask" ? 256 : 128;
}

// Kinds named by a path, by an id, or by both (webproto Attach.check).
const PATH_KINDS = new Set(["file", "lines", "diff"]);
const MAX_LABEL = 48;

export const composerRefs = signal<readonly RefChip[]>([]);

// Files that may hold secrets stay on the laptop (the laptop checks again).
const SECRET_FILES = [
  /(^|\/)\.env(\.[^/]*)?$/i,
  /(^|\/)\.envrc$/i,
  /(^|\/)\.netrc$/i,
  /(^|\/)\.npmrc$/i,
  /(^|\/)\.pypirc$/i,
  /(^|\/)\.git-credentials$/i,
  /(^|\/)\.ssh(\/|$)/i,
  /(^|\/)\.aws(\/|$)/i,
  /(^|\/)\.gnupg(\/|$)/i,
  /(^|\/)\.kube\/config$/i,
  /(^|\/)\.docker\/config\.json$/i,
  /(^|\/)id_(rsa|dsa|ecdsa|ed25519)(\.pub)?$/i,
  /\.(pem|key|p12|pfx|jks|keystore|kdbx|ovpn|age|gpg|asc)$/i,
  /(^|\/)credentials(\.[a-z]+)?$/i,
  /(^|\/)[^/]*secrets?[^/]*\.(ya?ml|json|toml|ini|txt)$/i,
];

/** secretPath says whether a path names a file that may hold secrets. */
export function secretPath(path: string): boolean {
  return SECRET_FILES.some((re) => re.test(path));
}

/** cleanRef copies the four reference fields of r and checks them, or
 * says null: an unknown kind, an over-long id, an absolute path or one
 * with "..". Nothing else of r is ever copied. */
export function cleanRef(r: ChatRef | null | undefined): ChatRef | null {
  if (!r || typeof r.kind !== "string" || !KINDS.has(r.kind)) return null;
  const out: ChatRef = { kind: r.kind };
  if (typeof r.id === "string" && r.id) {
    if (r.id.length > idMax(r.kind)) return null;
    out.id = r.id;
  }
  if (typeof r.path === "string" && r.path) {
    const p = r.path;
    if (p.length > MAX_PATH || p.startsWith("/") || /^[A-Za-z]:[\\/]/.test(p) || p.split(/[\\/]/).includes("..")) return null;
    out.path = p;
  }
  if (typeof r.line === "number" && Number.isInteger(r.line) && r.line > 0) out.line = r.line;
  if (!out.id && !out.path && out.kind !== "diff") return null;
  return out;
}

/** Attach is one entry of chat.send's attach list (webproto Attach). */
export interface Attach {
  kind: string;
  id?: string;
  path?: string;
  from?: number;
  to?: number;
  label?: string;
}

/** toAttach turns a clean ref into the attach the laptop takes: a file
 * with a line is a one-line lines attach; a path kind sends no id and an
 * id kind no path; a view needs its spec (Customize sends it), so a bare
 * view ref is not sent. */
export function toAttach(r: ChatRef, label: string): Attach | null {
  const name = label && !looksSecret(label) ? { label: label.slice(0, MAX_LABEL_SENT) } : {};
  if (PATH_KINDS.has(r.kind)) {
    if (r.kind === "diff") return { kind: "diff", ...(r.path ? { path: r.path } : {}), ...name };
    if (!r.path) return null;
    if (r.line) return { kind: "lines", path: r.path, from: r.line, to: r.line, ...name };
    return { kind: r.kind === "lines" ? "file" : r.kind, path: r.path, ...name };
  }
  if (r.kind === "view" || !r.id) return null;
  if (r.kind === "ptask") return r.path ? { kind: "ptask", id: r.id, path: r.path, ...name } : null;
  return { kind: r.kind, id: r.id, ...name };
}

/** refKey is one ref's identity: the same thing is never attached twice. */
export function refKey(r: ChatRef): string {
  return [r.kind, r.id ?? "", r.path ?? "", r.line ?? 0].join("\u0000");
}

function base(path: string): string {
  const i = path.lastIndexOf("/");
  return i >= 0 ? path.slice(i + 1) : path;
}

/** refLabel is the chip text: the row's title, unless it looks like a
 * secret, then the kind and the id or file name. */
export function refLabel(r: ChatRef, title?: string): string {
  let s = (title ?? "").replace(/\s+/g, " ").trim();
  if (!s || looksSecret(s)) s = [r.kind, r.path ? base(r.path) : r.id].filter(Boolean).join(" ");
  if (r.line && r.path) s += ":" + r.line;
  return s.length > MAX_LABEL ? s.slice(0, MAX_LABEL - 1) + "…" : s;
}

export type AddResult = "added" | "already" | "full" | "secret" | "bad";

/** addRef puts r into the composer. */
export function addRef(r: ChatRef | null | undefined, title?: string): AddResult {
  const c = cleanRef(r);
  if (!c) return "bad";
  if (c.path && secretPath(c.path)) return "secret";
  if (!toAttach(c, "")) return "bad";
  const key = refKey(c);
  const now = composerRefs.peek();
  if (now.some((x) => x.key === key)) return "already";
  if (now.length >= MAX_REFS) return "full";
  composerRefs.value = [...now, { key, ref: c, label: refLabel(c, title) }];
  return "added";
}

/** hasRef says whether r already waits in the composer. */
export function hasRef(r: ChatRef | null | undefined): boolean {
  const c = cleanRef(r);
  if (!c) return false;
  const key = refKey(c);
  return composerRefs.value.some((x) => x.key === key);
}

/** removeRef takes one ref (by ChatRef or key) out of the composer. */
export function removeRef(r: ChatRef | string): void {
  let key: string;
  if (typeof r === "string") key = r;
  else {
    const c = cleanRef(r);
    if (!c) return;
    key = refKey(c);
  }
  composerRefs.value = composerRefs.peek().filter((x) => x.key !== key);
}

export function clearRefs(): void {
  if (composerRefs.peek().length) composerRefs.value = [];
}

/** attachArgs is the attach list of chat.send, or undefined when nothing
 * waits (so a plain message stays exactly as before). */
export function attachArgs(): Attach[] | undefined {
  const out: Attach[] = [];
  for (const x of composerRefs.peek()) {
    const c = cleanRef(x.ref);
    const a = c && toAttach(c, x.label);
    if (a) out.push(a);
  }
  return out.length ? out.slice(0, MAX_REFS) : undefined;
}

/** A block as far as a ref needs it (webproto Block). */
interface BlockLike {
  id: string;
  ref?: ChatRef | null;
}

/** An item as far as a ref needs it (webproto BlockItem). */
interface ItemLike {
  id: string;
  ref?: ChatRef | null;
}

/** blockRef is the ref of a whole block: the thing it stands for, else
 * the block itself (the laptop keeps the blocks it sent). */
export function blockRef(b: BlockLike): ChatRef | null {
  return cleanRef(b.ref) ?? cleanRef({ kind: "block", id: b.id });
}

/** itemRef is the ref of one list, group or board item: its own thing,
 * else the item inside its block. */
export function itemRef(b: BlockLike, it: ItemLike): ChatRef | null {
  return cleanRef(it.ref) ?? (it.id ? cleanRef({ kind: "block", id: b.id + "/" + it.id }) : null) ?? blockRef(b);
}

/** rowRef is the ref of one table row (rows carry no ref of their own):
 * the block id and the row's number, from 1 ("b1#3"). */
export function rowRef(b: BlockLike, index: number): ChatRef | null {
  return cleanRef({ kind: "block", id: b.id + "#" + (index + 1) });
}
