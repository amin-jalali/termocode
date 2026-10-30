// The chat view (WEB-14, web UX 5.2): the chat open on the laptop, live.
// User messages on the right, the AI's answers with the ✦ label and their
// markdown from the AST, tool lines in mono, a cursor while an answer
// streams. Every message follows its own text direction (dir=auto); code
// stays left to right.
import { Fragment, h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import type { ChatItem, ChatMsg, ChatSnap } from "../../../../webproto/proto";
import { fetchMsg, full, msgKey } from "../lib/fullmsg";
import { toast } from "../lib/toasts";
import { ReqError } from "../net/link";
import { link, live, store } from "../session";
import { isStub, readChat } from "../state/chat";
import { t } from "../strings";
import { Md } from "../ui/Markdown";
import { Progress, SkeletonBubbles } from "../ui/Skeleton";
import { errText } from "../ui/CardSheet";
import { ChatList } from "./ChatList";
import { Composer } from "./Composer";
import { Run } from "./Run";
import { View } from "./View";

/** newChat asks the laptop for an empty chat; refused while the AI answers. */
function newChat(): void {
  link.request("chat.new").catch((e: unknown) => {
    if (e instanceof ReqError) toast("warn", e.code === "busy" ? t("chat.new_busy") : errText(e));
  });
}

/** NEAR_BOTTOM_PX: closer than this to the end, new text keeps the view at the end. */
const NEAR_BOTTOM_PX = 80;
/** PAGE is how many messages draw at first and per Load older. */
const PAGE = 60;

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

/** Extras: the attached image names and the rules file the turn used. */
function Extras({ m }: { m: ChatMsg }) {
  const imgs = m.images ?? [];
  if (imgs.length === 0 && !m.rules) return null;
  return (
    <div class="msg-extras" dir="ltr">
      {imgs.map((name, i) => (
        <span key={i} class="chip">
          🖼 {name}
        </span>
      ))}
      {m.rules && <span class="chip">{t("chat.rules", { name: m.rules })}</span>}
    </div>
  );
}

/** Stub is an older message sent without its text: it fetches itself
 * when it scrolls near the view (only for the laptop's own chat). */
function Stub({ m, chat, canFetch }: { m: ChatMsg; chat: string; canFetch: boolean }) {
  const ref = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (!canFetch) return;
    const el = ref.current;
    if (!el || typeof IntersectionObserver === "undefined") {
      fetchMsg(chat, m.i);
      return;
    }
    const io = new IntersectionObserver(
      (es) => {
        if (es.some((e) => e.isIntersecting)) fetchMsg(chat, m.i);
      },
      { root: el.closest(".view-body"), rootMargin: "200px 0px" },
    );
    io.observe(el);
    return () => io.disconnect();
  }, [chat, m.i, canFetch, full.value]);
  return (
    <div ref={ref} class={`msg msg-stub ${m.role === "user" ? "msg-user" : "msg-ai"}`} data-i={m.i}>
      {canFetch ? <span class="sk-row" aria-hidden="true" /> : <span class="stub-text">{t("chat.stub_laptop")}</span>}
    </div>
  );
}

export function Message({ m: raw, chat = "", canFetch = false }: { m: ChatMsg; chat?: string; canFetch?: boolean }) {
  const m = (raw.truncated && full.value.get(msgKey(chat, raw.i))) || raw;
  if (isStub(m)) return <Stub m={m} chat={chat} canFetch={canFetch} />;
  const rest = m.truncated && canFetch && (
    <button type="button" class="msg-rest" onClick={() => fetchMsg(chat, m.i)}>
      {t("chat.rest")}
    </button>
  );
  switch (m.role) {
    case "user":
      return (
        <div class="msg msg-user" data-i={m.i}>
          <div class="bubble" dir="auto">
            {m.md ? <Md doc={m.md} /> : <MsgText text={m.text} />}
            {rest}
          </div>
          <Extras m={m} />
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
            {rest}
          </div>
          <Run m={m} />
          <Extras m={m} />
        </div>
      );
  }
}

/** pinned is a stored chat the page reads instead of following the
 * laptop's (chat.open + chat.pin); null while following. */
export const pinned = signal<ChatSnap | null>(null);

/** follow goes back to the laptop's chat. */
export function follow(): void {
  pinned.value = null;
  link.request("chat.pin", { on: false }).catch(() => undefined); // advisory only
}

/** openChat shows a chat: the laptop's is followed, another is read here. */
function openChat(c: ChatItem, laptop: string): void {
  if (c.open || c.id === laptop) {
    if (pinned.peek()) follow();
    return;
  }
  link.request("chat.open", { id: c.id }).then(
    (data) => {
      const snap = readChat(data);
      if (!snap) return;
      pinned.value = snap;
      link.request("chat.pin", { on: true }).catch(() => undefined);
    },
    (e: unknown) => {
      if (e instanceof ReqError) toast("warn", e.code === "not_found" ? t("chat.gone") : errText(e));
    },
  );
}

export function ChatView() {
  useEffect(() => link.subscribe("chat", "cards", "chats"), []);
  const body = useRef<HTMLDivElement | null>(null);
  const stick = useRef(true);
  const [list, setList] = useState(false);
  const [page, setPage] = useState({ chat: "", n: PAGE });
  const keep = useRef<number | null>(null);
  const st = store.topic("chat").value;
  const laptop = readChat(st?.data);
  const pin = pinned.value;
  const chat = pin ?? laptop;
  const msgs = chat?.messages ?? [];
  const shownN = page.chat === (chat?.id ?? "") ? page.n : PAGE;
  const from = Math.max(0, msgs.length - shownN);

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
    if (!el) return;
    if (keep.current !== null) {
      // Load older: what the reader saw stays where it was.
      el.scrollTop += el.scrollHeight - keep.current;
      keep.current = null;
    } else if (stick.current && !list) el.scrollTop = el.scrollHeight;
  });

  const older = () => {
    keep.current = body.current?.scrollHeight ?? null;
    setPage({ chat: chat?.id ?? "", n: shownN + PAGE });
  };

  let content;
  if (list) {
    content = (
      <ChatList
        current={chat?.id ?? ""}
        onPick={(c) => {
          setList(false);
          stick.current = true;
          openChat(c, laptop?.id ?? "");
        }}
      />
    );
  } else if (!st && !pin) {
    content = (
      <div class="loading">
        <Progress k="chat" />
        <SkeletonBubbles />
      </div>
    );
  } else {
    content = (
      <div class={`topic chat${st?.stale && !pin ? " is-stale" : ""}`}>
        {msgs.length === 0 ? (
          <div class="empty">
            <div class="empty-state">
              <span class="empty-mark" aria-hidden="true">✦</span>
              <p>{t("chat.empty")}</p>
            </div>
          </div>
        ) : (
          <div class="msgs" role="log" aria-live="polite" aria-label={chat?.title || t("tab.chat")}>
            {from > 0 && (
              <button type="button" class="chat-older" onClick={older}>
                {t("chat.older")}
              </button>
            )}
            {msgs.slice(from).map((m) => (
              <Message key={m.i} m={m} chat={chat?.id ?? ""} canFetch={!pin} />
            ))}
          </div>
        )}
      </div>
    );
  }
  let foot;
  if (pin) {
    foot = (
      <div class="composer">
        <p class="composer-note" dir="auto">
          {t("chat.conflict")}
        </p>
      </div>
    );
  } else if (st) {
    foot = <Composer chat={laptop?.id ?? ""} busy={laptop?.busy ?? false} />;
  }
  return (
    <View
      tab="chat"
      title={t("tab.chat")}
      sub={chat?.title ? <span class="trunc" dir="auto">{chat.title}</span> : undefined}
      actions={
        <>
          {pin && <span class="chat-pinned">📌 {t("chat.pinned")}</span>}
          <button type="button" class={`head-btn chat-list-btn${list ? " is-on" : ""}`} aria-pressed={list ? "true" : "false"} onClick={() => setList(!list)}>
            {t("chat.list")}
          </button>
          {pin ? (
            <button type="button" class="head-btn chat-follow" onClick={follow}>
              {t("chat.follow")}
            </button>
          ) : (
            <button type="button" class="head-btn chat-new" onClick={newChat}>
              {t("chat.new")}
            </button>
          )}
        </>
      }
      bodyRef={body}
      foot={foot}
    >
      {content}
    </View>
  );
}
