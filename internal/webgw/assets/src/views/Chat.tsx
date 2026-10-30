// The chat view (WEB-14, web UX 5.2): the chat open on the laptop, live.
// User messages on the right, the AI's answers with the ✦ label and their
// markdown from the AST, tool lines in mono, a cursor while an answer
// streams. Every message follows its own text direction (dir=auto); code
// stays left to right.
import { h } from "preact";
import { useEffect, useLayoutEffect, useRef } from "preact/hooks";
import type { ChatMsg } from "../../../../webproto/proto";
import { link, live, store } from "../session";
import { readChat } from "../state/chat";
import { t } from "../strings";
import { Md } from "../ui/Markdown";
import { Progress, SkeletonBubbles } from "../ui/Skeleton";
import { View } from "./View";

/** NEAR_BOTTOM_PX: closer than this to the end, new text keeps the view at the end. */
const NEAR_BOTTOM_PX = 80;

function MsgText({ text, streaming }: { text?: string; streaming?: boolean }) {
  return (
    <p class="msg-text" dir="auto">
      {text ?? ""}
      {streaming && (
        <span class={`cursor${live.value ? "" : " is-paused"}`} aria-hidden="true">
          ▍
        </span>
      )}
      {streaming && !live.value && <span class="msg-paused">{t("chat.paused")}</span>}
    </p>
  );
}

export function Message({ m }: { m: ChatMsg }) {
  switch (m.role) {
    case "user":
      return (
        <div class="msg msg-user" data-i={m.i}>
          <div class="bubble" dir="auto">
            {m.md ? <Md doc={m.md} /> : <MsgText text={m.text} />}
          </div>
        </div>
      );
    case "tool":
      return (
        <div class="msg msg-tool" data-i={m.i} dir="ltr">
          <span class="tool-mark" aria-hidden="true">⚙</span>
          <span class="tool-text">{m.text ?? ""}</span>
        </div>
      );
    case "error":
      return (
        <div class="msg msg-error" data-i={m.i} dir="auto">
          {t("chat.error", { text: m.text ?? "" })}
        </div>
      );
    default:
      return (
        <div class="msg msg-ai" data-i={m.i}>
          <div class="msg-label">
            <span class="star" aria-hidden="true">✦</span>
            <span>{t("chat.ai")}</span>
            {m.model && <span class="msg-model">{m.model}</span>}
          </div>
          <div class="msg-body">
            {m.md && !m.streaming ? <Md doc={m.md} /> : <MsgText text={m.text} streaming={m.streaming} />}
          </div>
        </div>
      );
  }
}

export function ChatView() {
  useEffect(() => link.subscribe("chat", "cards", "chats"), []);
  const body = useRef<HTMLDivElement | null>(null);
  const stick = useRef(true);
  const st = store.topic("chat").value;
  const chat = readChat(st?.data);
  const msgs = chat?.messages ?? [];

  useEffect(() => {
    const el = body.current;
    if (!el) return;
    const onScroll = () => (stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < NEAR_BOTTOM_PX);
    el.addEventListener("scroll", onScroll, { passive: true });
    return () => el.removeEventListener("scroll", onScroll);
  }, []);
  // New text keeps the view at the end while the reader is there.
  useLayoutEffect(() => {
    const el = body.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  });

  let content;
  if (!st) {
    content = (
      <div class="loading">
        <Progress k="chat" />
        <SkeletonBubbles />
      </div>
    );
  } else {
    content = (
      <div class={`topic chat${st.stale ? " is-stale" : ""}`}>
        {msgs.length === 0 ? (
          <div class="empty">
            <div class="empty-state">
              <span class="empty-mark" aria-hidden="true">✦</span>
              <p>{t("chat.empty")}</p>
            </div>
          </div>
        ) : (
          <div class="msgs" role="log" aria-live="polite" aria-label={chat?.title || t("tab.chat")}>
            {msgs.map((m) => (
              <Message key={m.i} m={m} />
            ))}
          </div>
        )}
      </div>
    );
  }
  return (
    <View tab="chat" title={t("tab.chat")} sub={chat?.title ? <span class="trunc" dir="auto">{chat.title}</span> : undefined} bodyRef={body}>
      {content}
    </View>
  );
}
