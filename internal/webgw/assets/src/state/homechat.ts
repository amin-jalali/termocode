// The phone's own chat (MOB-16, chat.home): on a phone the Home's
// composer sends to a background "Phone · <device>" chat of the
// workspace, so the laptop's open chat and view stay still. The laptop
// answers with its main model (no tools) and replies with the whole phone
// chat once the answer is in; chat.home {} reads it back (no tokens).
// Pure logic: the Home hands in its request function (src/home/api.ts),
// so this file holds no session and runs in any JS engine.
import type { ChatSnap } from "../../../../webproto/proto";

/** Request is the page link's request (link.request). */
export type Request = (op: string, args?: unknown) => Promise<unknown>;

/** HomeChat is what the Home shows of the phone chat. */
export interface HomeChat {
  chat: ChatSnap | null; // null: not read yet
  sending: string; // the message on its way ("" when none)
  error: string; // the last failure, in the laptop's words
}

export const emptyHomeChat: HomeChat = { chat: null, sending: "", error: "" };

/** homeChatRead reads the phone chat back. */
export function homeChatRead(request: Request): Promise<HomeChat> {
  return request("chat.home", {}).then(
    (r) => ({ chat: r as ChatSnap, sending: "", error: "" }),
    (e: unknown) => ({ ...emptyHomeChat, error: errText(e) }),
  );
}

/** homeChatSending is the state while text is on its way: the Home shows
 * it at once, greyed, until the laptop answers. */
export function homeChatSending(s: HomeChat, text: string): HomeChat {
  return { ...s, sending: text.trim(), error: "" };
}

/** homeChatSend sends text; the result is the phone chat with the answer,
 * or the old chat with the laptop's reason (busy, stopped, not set up).
 * A failed send keeps the text so the composer can put it back. */
export function homeChatSend(request: Request, s: HomeChat, text: string): Promise<HomeChat> {
  const msg = text.trim();
  if (msg === "") return Promise.resolve(s);
  return request("chat.home", { text: msg }).then(
    (r) => ({ chat: r as ChatSnap, sending: "", error: "" }),
    (e: unknown) => ({ ...s, sending: "", error: errText(e) }),
  );
}

/** homeChatBusy: the phone chat is answering (the composer's Send turns
 * into Stop, which is chat.stop). */
export function homeChatBusy(s: HomeChat): boolean {
  return s.sending !== "" || (s.chat?.busy ?? false);
}

function errText(e: unknown): string {
  return e instanceof Error && e.message !== "" ? e.message : "The phone chat failed. Try again.";
}
