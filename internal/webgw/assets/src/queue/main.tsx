// The phone remote's queue (queue.js, CMP-46): while the laptop's AI
// answers in its open chat, the composer lines messages up (chat.queue
// {text}); the laptop sends the first when the turn ends. Each waiting
// message has Send now ({now}: the turn stops and it goes next) and Take
// back ({remove}). Stop drops the queue on the laptop; the chat topic's
// state delta then shows it empty. At most 5 wait. The page loads this
// file (lib/queue.ts) and gives it Preact's h, the link and the strings;
// it imports no value of the page.
import type { JSX, h as preactH } from "preact";
import type { ChatQueued } from "../../../../webproto/proto";
import { QS } from "./strings";

/** QueueHost is what the page gives queue.js. */
export interface QueueHost {
  h: typeof preactH;
  request: (op: string, args: unknown) => Promise<unknown>;
  t: (key: string, vars?: Record<string, string | number>) => string;
  addStrings: (d: Readonly<Record<string, string>>) => void;
  /** isBusy: the laptop said busy (here: the queue is full). */
  isBusy: (e: unknown) => boolean;
}

let H!: QueueHost;
// JSX here compiles to h(…), the page's own.
let h: QueueHost["h"];

/** init takes the page's host (once, on the first load). */
export function init(host: QueueHost): void {
  H = host;
  h = host.h;
  host.addStrings(QS);
}

/** MAX is how many messages may wait (webproto.MaxChatQueue). */
export const MAX = 5;

const op = (args: Record<string, string>) => H.request("chat.queue", args);

/** add lines text up after the running answer. */
export const add = (text: string) => op({ text });
/** now steers: the turn stops and id goes next. */
export const now = (id: string) => op({ now: id });
/** remove takes id back. */
export const remove = (id: string) => op({ remove: id });
/** full: no more may wait. */
export const full = (items: readonly ChatQueued[]) => items.length >= MAX;
/** note is the composer's line for a refused add. */
export const note = (e: unknown) => H.t(H.isBusy(e) ? "queue.full" : "err.internal");

/** Queue draws the waiting messages, first to go first; nothing when
 * none waits. */
export function Queue({ items }: { items: readonly ChatQueued[] }): JSX.Element | null {
  if (!items.length) return null;
  return (
    <div class="composer-queue" role="group" aria-label={H.t("queue.title", { n: items.length })}>
      <p class="composer-note">{H.t("queue.title", { n: items.length })}</p>
      <ol>
        {items.map((q) => (
          <li key={q.id} class="chat-chip">
            <span dir="auto" class="trunc">
              {q.text}
            </span>
            <button type="button" class="btn btn-quiet btn-small" onClick={() => void now(q.id).catch(() => undefined)}>
              {H.t("queue.now")}
            </button>
            <button type="button" class="btn btn-quiet btn-small" onClick={() => void remove(q.id).catch(() => undefined)}>
              {H.t("queue.back")}
            </button>
          </li>
        ))}
      </ol>
    </div>
  );
}
