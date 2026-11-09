// The chat composer (web UX 5.2): a growing text box with Send; while the
// AI answers Send becomes Stop (stopping is never harmful). Offline both
// read "Offline" and do nothing, and the draft stays (P5). A refusal from
// the laptop (the chat is held elsewhere, the AI is busy) shows one calm
// line and keeps the draft. onSent runs after a send was taken (an old
// chat the page read is then the laptop's open one). Enter sends with a keyboard; on a phone Enter
// is a new line and the Send button sends. Where the browser has speech
// input, a microphone button dictates into the draft (CMP-48, dictate.ts);
// nothing is sent until Send.
import { aiSnap } from "../lib/ai";
import { look } from "../lib/toolblocks";
import { h } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { getDraft, pendingDraft, setDraft, takePending } from "../lib/drafts";
import { attachLabel, clearAttaches, getAttaches, inProject, removeAttach } from "../lib/attach";
import { layout } from "../lib/layout";
import { ctxTone } from "../state/chat";
import { OfflineError, ReqError } from "../net/link";
import { link, live } from "../session";
import { t, type StringKey } from "../strings";
import { Button } from "../ui/Button";
import { Cod } from "../ui/icons";
import { errText } from "../ui/CardSheet";
import { canDictate, dictateChat, listening, stopDictate, toggleDictate } from "../lib/dictate";
import { attachArgs, clearRefs } from "../lib/tochat";
import { RefChips } from "../ui/RefChips";

const MAX_ROWS_PX = 160;

export function Composer({
  chat,
  busy,
  profile = "",
  model = "",
  ctx,
  onSent,
  placeholder,
  ws = "",
}: {
  chat: string;
  busy: boolean;
  profile?: string;
  model?: string;
  ctx?: { pct: number; tokens: number; window: number };
  onSent?: () => void;
  /** placeholder replaces the box's hint (the phone's Home asks "What do you want?"). */
  placeholder?: string;
  /** ws sends in another project through the door (MOB-55); "" is the home one. */
  ws?: string;
}) {
  const desk = layout.value === "desktop";
  useEffect(() => takePending(chat), [chat, pendingDraft.value]);
  const text = getDraft(chat);
  const [note, setNote] = useState<string | null>(null);
  const [sending, setSending] = useState(false);
  const box = useRef<HTMLTextAreaElement | null>(null);

  // The microphone writes to the chat this composer shows.
  useEffect(() => {
    dictateChat.value = chat;
    return () => {
      if (listening.peek() === chat) stopDictate();
      if (dictateChat.peek() === chat) dictateChat.value = null;
    };
  }, [chat]);
  const hearing = listening.value === chat;
  const mic = canDictate() && (
    <span class="mic">
      <Button kind="quiet" icon={<Cod name={hearing ? "mic-filled" : "mic"} />} iconOnly pressed={hearing} title={t(hearing ? "dictate.stop" : "dictate.start")} onClick={toggleDictate}>
        {t(hearing ? "dictate.stop" : "dictate.start")}
      </Button>
    </span>
  );

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
    const attach = getAttaches(chat); // MOB-5: references only, the laptop reads them
    // The refs from ↩ go with the text, as references only (MOBUX-19).
    const refs = attachArgs() ?? [];
    const sent = inProject([...attach, ...refs], ws); // MOB-55: one project per send (the laptop checks)
    const args = { text: body, ...(chat ? { chat_id: chat } : {}), ...(sent.length ? { attach: sent } : {}) };
    link.requestWithID("chat.send", args, ws || undefined).result.then(
      () => {
        setSending(false);
        setNote(null);
        if (getDraft(chat) === body) setDraft(chat, "");
        if (getAttaches(chat) === attach) clearAttaches(chat);
        if (refs.length) clearRefs();
        onSent?.();
      },
      (e: unknown) => {
        setSending(false);
        if (e instanceof OfflineError) return; // the box shows Offline; the draft stays
        const key: StringKey | null = e instanceof ReqError ? (e.code === "conflict" ? "chat.conflict" : e.code === "busy" ? "chat.busy" : e.code === "not_found" ? "chat.gone" : null) : null;
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
      {!desk && <RefChips />}
      <div class={desk ? "composer-frame" : "composer-plain"}>
      {getAttaches(chat).length > 0 && (
        <ul class="composer-attach" aria-label={t("ai.attach_list")}>
          {getAttaches(chat).map((a, i) => (
            <li class="chat-chip" key={i}>
              <span dir="auto">{attachLabel(a)}</span>
              <button type="button" class="chip-x" aria-label={`Remove ${attachLabel(a)}`} onClick={() => removeAttach(chat, i)}>
                ×
              </button>
            </li>
          ))}
        </ul>
      )}
      {!desk && <span class="chat-picks">{look.value?.pick()}</span>}
      <div class="composer-row">
        <textarea
          ref={box}
          class="composer-box"
          dir="auto"
          rows={1}
          value={text}
          placeholder={placeholder ?? t("chat.placeholder")}
          aria-label={placeholder ?? t("chat.placeholder")}
          onInput={(e) => {
            setDraft(chat, (e.currentTarget as HTMLTextAreaElement).value);
            if (note) setNote(null);
          }}
          onKeyDown={onKeyDown}
          onKeyPress={onKeyPress}
        />
        {!desk && mic}
        {!desk && (
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
        )}
      </div>
      {desk && (
        <div class="composer-tools">
          {aiSnap()?.configured ? (
            <span class="chat-picks">{look.value?.pick()}</span>
          ) : (
            <span class="chat-chip" title={t("chat.agent")}>
              <Cod name="sparkle" /> {[profile || t("chat.default_profile"), model].filter(Boolean).join(" · ")}
            </span>
          )}
          {ctx && ctx.pct > 0 && (
            <span class={"chat-chip ctx-chip ctx-" + ctxTone(ctx.pct)} title={t("chat.ctx_title", { tokens: ctx.tokens, window: ctx.window, pct: ctx.pct })}>
              <span class="ctx-bar" aria-hidden="true">
                <span style={{ width: Math.min(ctx.pct, 100) + "%" }} />
              </span>
              {t("chat.ctx", { pct: ctx.pct })}
            </span>
          )}
          {mic}
          <span class={busy ? "stop" : "send"}>
            {busy ? (
              <Button kind="quiet" needsLink icon={<Cod name="debug-stop" />} iconOnly title={t("stop")} onClick={stop}>
                {t("stop")}
              </Button>
            ) : (
              <Button kind="quiet" needsLink icon={<Cod name="send" />} iconOnly title={`${t("send")} (Enter)`} disabled={sending || !text.trim()} onClick={send}>
                {t("send")}
              </Button>
            )}
          </span>
        </div>
      )}
      </div>
      <div class="composer-hint" aria-hidden="true">
        {t("chat.hint")}
      </div>
    </div>
  );
}
