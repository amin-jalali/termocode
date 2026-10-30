// The laptop's AI in the desktop editor (WEBP-3). Everything runs on the
// laptop with its own provider, model, prompts and settings; the page
// only says where and shows the answer. Ghost text and next edits are
// asked by the editor's frame (lib/deskeditor.ts) at the tick the page's
// text is known at (unsent typing goes first).
import type { AICompletion, AISnap } from "../../../../webproto/proto";
import type { AiKind } from "../monaco/protocol";
import { ReqError } from "../net/link";
import { link, store } from "../session";
import { errText } from "../ui/CardSheet";
import { syncs } from "./live";
import { toast } from "./toasts";

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
