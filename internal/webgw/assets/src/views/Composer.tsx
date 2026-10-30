// The chat composer (web UX 5.2): a growing text box with Send; while the
// AI answers Send becomes Stop (stopping is never harmful). Offline both
// read "Offline" and do nothing, and the draft stays (P5). A refusal from
// the laptop (another chat is open there, the AI is busy) shows one calm
// line and keeps the draft. Enter sends with a keyboard; on a phone Enter
// is a new line and the Send button sends.
import { h } from "preact";
import { useLayoutEffect, useRef, useState } from "preact/hooks";
import { getDraft, setDraft } from "../lib/drafts";
import { layout } from "../lib/layout";
import { OfflineError, ReqError } from "../net/link";
import { link, live } from "../session";
import { t, type StringKey } from "../strings";
import { Button } from "../ui/Button";
import { errText } from "../ui/CardSheet";

const MAX_ROWS_PX = 160;

export function Composer({ chat, busy }: { chat: string; busy: boolean }) {
  const text = getDraft(chat);
  const [note, setNote] = useState<string | null>(null);
  const [sending, setSending] = useState(false);
  const box = useRef<HTMLTextAreaElement | null>(null);

  // The box grows with its text up to about four lines, then scrolls.
  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, MAX_ROWS_PX) + "px";
  }, [text]);

  const send = () => {
    const body = getDraft(chat);
    if (!live.peek() || sending || !body.trim()) return;
    setSending(true);
    link.request("chat.send", { text: body, ...(chat ? { chat_id: chat } : {}) }).then(
      () => {
        setSending(false);
        setNote(null);
        if (getDraft(chat) === body) setDraft(chat, "");
      },
      (e: unknown) => {
        setSending(false);
        if (e instanceof OfflineError) return; // the box shows Offline; the draft stays
        const key: StringKey | null = e instanceof ReqError ? (e.code === "conflict" ? "chat.conflict" : e.code === "busy" ? "chat.busy" : null) : null;
        setNote(key ? t(key) : errText(e));
      },
    );
  };

  const stop = () => {
    link.request("chat.stop").catch(() => undefined); // stopping is answered ok; offline shows on the button
  };

  const sendKey = (e: KeyboardEvent) => e.key === "Enter" && !e.shiftKey && !e.isComposing && layout.peek() !== "phone";
  const onKeyDown = (e: KeyboardEvent) => {
    if (!sendKey(e)) return;
    e.preventDefault();
    send();
  };
  // Some keyboards still deliver the Enter as a character: it is not text.
  const onKeyPress = (e: KeyboardEvent) => {
    if (sendKey(e)) e.preventDefault();
  };

  return (
    <div class="composer">
      {note && (
        <p class="composer-note" role="alert" dir="auto">
          {note}
        </p>
      )}
      <div class="composer-row">
        <textarea
          ref={box}
          class="composer-box"
          dir="auto"
          rows={1}
          value={text}
          placeholder={t("chat.placeholder")}
          aria-label={t("chat.placeholder")}
          onInput={(e) => {
            setDraft(chat, (e.currentTarget as HTMLTextAreaElement).value);
            if (note) setNote(null);
          }}
          onKeyDown={onKeyDown}
          onKeyPress={onKeyPress}
        />
        <span class={busy ? "stop" : "send"}>
          {busy ? (
            <Button kind="neutral" needsLink onClick={stop}>
              {t("stop")}
            </Button>
          ) : (
            <Button kind="primary" needsLink disabled={sending || !text.trim()} onClick={send}>
              {t("send")}
            </Button>
          )}
        </span>
      </div>
      <div class="composer-hint" aria-hidden="true">
        {t("chat.hint")}
      </div>
    </div>
  );
}
