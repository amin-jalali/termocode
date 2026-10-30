// Whole messages fetched with chat.get_message (web UX 5.2 G): older
// messages that arrived as stubs, and messages cut at 64 KiB. At most two
// requests run at once; each message is asked for once while in flight.
// The answer is for the chat open on the laptop, so a page that reads a
// pinned chat never asks.
import { signal } from "@preact/signals";
import type { ChatMsg } from "../../../../webproto/proto";
import { link, store } from "../session";
import { readChat, readMsg } from "../state/chat";

const MAX_RUNNING = 2;

/** full holds fetched messages by "<chat id>#<index>". */
export const full = signal<ReadonlyMap<string, ChatMsg>>(new Map());

const queue: { chat: string; i: number }[] = [];
const asked = new Set<string>();
let running = 0;

export function msgKey(chat: string, i: number): string {
  return chat + "#" + i;
}

/** fetchMsg asks for message i of the laptop's open chat. */
export function fetchMsg(chat: string, i: number): void {
  const k = msgKey(chat, i);
  if (asked.has(k)) return;
  asked.add(k);
  queue.push({ chat, i });
  pump();
}

function pump(): void {
  while (running < MAX_RUNNING && queue.length > 0) {
    const { chat, i } = queue.shift()!;
    const k = msgKey(chat, i);
    running++;
    link
      .request("chat.get_message", { i })
      .then((data) => {
        const m = readMsg(data);
        // The laptop may have opened another chat meanwhile.
        if (!m || m.i !== i || readChat(store.topic("chat").peek()?.data)?.id !== chat) return;
        const next = new Map(full.peek());
        next.set(k, m);
        full.value = next;
      })
      .catch(() => undefined) // left as it was; it is asked again when it shows again
      .finally(() => {
        running--;
        asked.delete(k);
        pump();
      });
  }
}
