// The composer's attach chips (MOB-5): items the user sends back with the
// next message, by reference only (webproto.Attach). The laptop resolves
// each one under its secret rules; the page never reads or sends an
// item's content. Kept in memory for the life of the page, one list per
// chat, at most 8 (MaxAttach); nothing is stored on the device.
import { signal } from "@preact/signals";
import type { Attach } from "../../../../webproto/proto";

export const MAX_ATTACH = 8;

export const attaches = signal<Readonly<Record<string, readonly Attach[]>>>({});

/** sameAttach: two chips point at the same item. */
export function sameAttach(a: Attach, b: Attach): boolean {
  return a.kind === b.kind && (a.id ?? "") === (b.id ?? "") && (a.path ?? "") === (b.path ?? "") && (a.from ?? 0) === (b.from ?? 0) && (a.to ?? 0) === (b.to ?? 0) && (a.spec ?? "") === (b.spec ?? "");
}

export function getAttaches(chat: string): readonly Attach[] {
  return attaches.value[chat] ?? [];
}

/** addAttach adds a chip; false when the list is full (the item is not
 * added) — the same item twice stays one chip. */
export function addAttach(chat: string, a: Attach): boolean {
  const cur = attaches.peek()[chat] ?? [];
  if (cur.some((x) => sameAttach(x, a))) return true;
  if (cur.length >= MAX_ATTACH) return false;
  attaches.value = { ...attaches.peek(), [chat]: [...cur, a] };
  return true;
}

export function removeAttach(chat: string, i: number): void {
  const cur = attaches.peek()[chat] ?? [];
  const next = { ...attaches.peek() };
  const left = cur.filter((_, j) => j !== i);
  if (left.length) next[chat] = left;
  else delete next[chat];
  attaches.value = next;
}

/** clearAttaches drops the chips once a send was taken. */
export function clearAttaches(chat: string): void {
  if (!(chat in attaches.peek())) return;
  const next = { ...attaches.peek() };
  delete next[chat];
  attaches.value = next;
}

/** attachLabel is a chip's text: the label it came with, else one made
 * from the reference. */
export function attachLabel(a: Attach): string {
  if (a.label) return a.label;
  switch (a.kind) {
    case "file":
      return a.path ?? "file";
    case "lines":
      return `${a.path ?? ""}:${a.from ?? 0}-${a.to ?? 0}`;
    case "diff":
      return a.path ? `changes of ${a.path}` : "changes";
    case "issue":
      return `issue #${(a.id ?? "").replace(/^#/, "")}`;
    case "pr":
      return `pull request ${a.id ?? ""}`;
    default:
      return `${a.kind} ${a.id ?? ""}`.trim();
  }
}
