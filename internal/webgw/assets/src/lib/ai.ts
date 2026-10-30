// The laptop's AI in the desktop editor (WEBP-3). Everything runs on the
// laptop with its own provider, model, prompts and settings; the page
// only says where and shows the answer. Ghost text and next edits are
// asked by the editor's frame (lib/deskeditor.ts) at the tick the page's
// text is known at (unsent typing goes first). Explain asks in the
// laptop's open chat. Fix, Edit and Doc answer with new lines, shown as
// a diff tab ("@ai:<n>:<path>") with an arrow per change to take it back;
// Accept writes what is left in one edit, guarded by the tick it was
// asked at (the file changed since: nothing is written).
import { signal } from "@preact/signals";
import type { AIAction, AICompletion, AISnap } from "../../../../webproto/proto";
import type { AiAct, AiAt, AiKind } from "../monaco/protocol";
import { ReqError } from "../net/link";
import { link, store } from "../session";
import { t } from "../strings";
import { errText } from "../ui/CardSheet";
import { aiTab } from "./difftab";
import { own } from "./edit";
import { syncs } from "./live";
import { diff, pos } from "./textdiff";
import { toast } from "./toasts";
import { closeTab, quick, setPanes, showFile } from "./workbench";

/** aiSnap is the ai topic (null: not here yet). */
export function aiSnap(): AISnap | null {
  const d = store.topic("ai").value?.data as AISnap | undefined;
  return d && Array.isArray(d.profiles) && Array.isArray(d.models) ? d : null;
}

/** hubSays is the hub's own words for a refusal (short English). */
export function hubSays(e: unknown): string {
  return e instanceof ReqError && e.message ? e.message : errText(e);
}

/** settled sends buf's unsent text and gives the tick and text the page
 * knows it at (null: unsent text, a conflict, or no editor of buf). */
async function settled(buf: number): Promise<{ tick: number; text: string } | null> {
  const s = [...syncs.peek().values()].find((x) => x.buf === buf);
  if (!s) return null;
  await s.flush();
  const tick = s.settled();
  return tick === null ? null : { tick, text: s.textNow() };
}

/** aiAsk answers the frame: ghost text at a caret, the next edit after
 * a pause, or the first copy for next edits (seed). */
export async function aiAsk(kind: AiKind, buf: number, line: number, col: number, manual: boolean): Promise<unknown> {
  const s = await settled(buf);
  if (!s) return null;
  try {
    if (kind !== "complete") return await link.request("ai.next_edit", { buf, tick: s.tick, line, seed: kind === "seed" || undefined });
    const d = (await link.request("ai.complete", { buf, tick: s.tick, line, col, manual: manual || undefined })) as AICompletion;
    if (manual && d.none && d.why) toast("info", d.why);
    return d;
  } catch (e) {
    if (manual) toast("warn", hubSays(e));
    return null;
  }
}

/** Proposal is an AI suggestion waiting in its diff tab. */
export interface Proposal {
  n: number;
  path: string;
  buf: number;
  /** tick and before: the file when it was asked. */
  tick: number;
  before: string;
  after: string;
  title: string;
}

export const proposals = signal<ReadonlyMap<number, Proposal>>(new Map());
/** aiBusy is the action the AI works on now (one at a time). */
export const aiBusy = signal<AiAct | null>(null);
let nextProposal = 0;

/** aiAction runs an editor action on path's buffer; Edit asks what to
 * change first. */
export function aiAction(kind: AiAct, path: string, buf: number, at: AiAt): void {
  if (kind !== "edit") return void runAction(kind, path, buf, at);
  quick.value = { mode: "ask", ask: { title: t("ai.edit_ask"), run: (text) => void runAction(kind, path, buf, at, text) } };
}

async function runAction(kind: AiAct, path: string, buf: number, at: AiAt, instruction?: string): Promise<void> {
  if (aiBusy.peek()) return void toast("info", t("ai.busy"));
  const s = await settled(buf);
  if (!s) return void toast("warn", t("ai.unsent"));
  aiBusy.value = kind;
  try {
    const r = (await link.request("ai.action", { buf, tick: s.tick, kind, start: at.start, end: at.end, line: at.line, instruction })) as AIAction;
    if (r.chat) {
      setPanes({ chat: true });
      return void toast("ok", t("ai.asked_chat"));
    }
    if (r.none) return void toast("info", t("ai.no_change"));
    const rows = s.text.split("\n");
    const after = [...rows.slice(0, r.start), ...r.lines, ...rows.slice(r.end)].join("\n");
    const n = ++nextProposal;
    proposals.value = new Map(proposals.peek()).set(n, { n, path, buf, tick: s.tick, before: s.text, after, title: r.title ?? "" });
    showFile(aiTab(n, path));
  } catch (e) {
    toast("warn", hubSays(e));
  } finally {
    aiBusy.value = null;
  }
}

function drop(p: Proposal): void {
  const next = new Map(proposals.peek());
  next.delete(p.n);
  proposals.value = next;
  closeTab(aiTab(p.n, p.path));
  showFile(p.path);
}

/** acceptProposal writes text (the suggestion, minus the changes the
 * user took back) as one edit at the tick it was asked at. */
export async function acceptProposal(p: Proposal, text: string): Promise<void> {
  const d = diff(p.before, text);
  if (d) {
    try {
      const r = (await link.request("buf.set_text", { buf: p.buf, tick: p.tick, start: pos(p.before, d.from), end: pos(p.before, d.to), lines: d.insert.split("\n") })) as { tick: number };
      own.tick = Math.max(own.tick, r.tick);
    } catch (e) {
      return void toast("warn", e instanceof ReqError && e.code === "conflict" ? t("ai.changed") : hubSays(e));
    }
  }
  drop(p);
  toast("ok", t("ai.applied"));
}

/** rejectProposal drops the suggestion; the file stays as it is. */
export function rejectProposal(p: Proposal): void {
  drop(p);
}
