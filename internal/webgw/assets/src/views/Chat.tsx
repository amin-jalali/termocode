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
import { BlobImg } from "../ui/BlobImg";
import { Md } from "../ui/Markdown";
import { Cod, face } from "../ui/icons";
import { layout } from "../lib/layout";
import { hasBlocks, look, toolBlocks } from "../lib/toolblocks";
import { Progress, SkeletonBubbles } from "../ui/Skeleton";
import { errText } from "../ui/CardSheet";
import { ChatList } from "./ChatList";
import { ChatMoreFlows, openMore } from "./ChatMore";
import { Composer } from "./Composer";
import { CommentBox } from "./ChatComment";
import { Run } from "./Run";
import { View } from "./View";

/** newChat asks the laptop for an empty chat; refused while the AI answers. */
export function newChat(): void {
  link.request("chat.new").catch((e: unknown) => {
    if (e instanceof ReqError) toast("warn", e.code === "busy" ? t("chat.new_busy") : errText(e));
  });
}

/** NEAR_BOTTOM_PX: closer than this to the end, new text keeps the view at the end. */
const NEAR_BOTTOM_PX = 80;
/** PAGE is how many messages draw at first and per Load older. */
const PAGE = 60;

/** CHUNK: a plain text draws in blocks of about this many characters,
 * each cut after a line end, so a streamed token lays out the last block
 * only (one 48 KB text node took 5.5 ms of layout a token; at 4x CPU
 * the page drew a token seconds late). The text only grows, so the cuts
 * stay where they were. */
const CHUNK = 1024;

function MsgText({ text = "", streaming }: { text?: string; streaming?: boolean }) {
  const parts = [];
  let i = 0;
  for (let j; (j = text.indexOf("\n", i + CHUNK)) >= 0; i = j + 1) parts.push(<div>{text.slice(i, j + 1)}</div>);
  return (
    <div class="msg-text" dir="auto">
      {parts}
      {text.slice(i)}
      {streaming && (
        <span class={`cursor${live.value ? "" : " is-paused"}`} aria-hidden="true">
          ▍
        </span>
      )}
      {streaming && !live.value && <span class="msg-paused">{t("chat.paused")}</span>}
    </div>
  );
}

/** Extras: the attached images and the rules file the turn used. In the
 * laptop's own chat a controller can tap an image to see it here. */
function Extras({ m, canFetch }: { m: ChatMsg; canFetch: boolean }) {
  const imgs = m.images ?? [];
  const [open, setOpen] = useState<number | null>(null);
  const sent = m.attached ?? []; // MOB-5: what came with it, by reference
  if (imgs.length === 0 && !m.rules && sent.length === 0) return null;
  const canSee = canFetch && link.meta.value?.role === "controller";
  return (
    <div class="msg-extras" dir="ltr">
      {imgs.map((name, i) =>
        canSee ? (
          <button key={i} type="button" class={`chip msg-img-chip${open === i ? " is-on" : ""}`} aria-expanded={open === i ? "true" : "false"} onClick={() => setOpen(open === i ? null : i)}>
            🖼 {name}
          </button>
        ) : (
          <span key={i} class="chip">
            🖼 {name}
          </span>
        ),
      )}
      {sent.map((a, i) => (
        <span key={"a" + i} class="chip" dir="auto">
          ↩ {a.label}
        </span>
      ))}
      {m.rules && <span class="chip rules">{t("chat.rules", { name: m.rules })}</span>}
      {open !== null && imgs[open] !== undefined && (
        <div class="msg-img">
          <BlobImg op="chat.image" args={{ i: m.i, n: open }} alt={t("img.alt", { name: imgs[open] })} />
        </div>
      )}
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
          <Extras m={m} canFetch={canFetch} />
        </div>
      );
    case "tool":
      return look.value?.m(m, null) ?? (
        <div class="msg msg-tool" data-i={m.i} dir="ltr">
          <span class="tool-mark" aria-hidden="true">⚙</span>
          <span class="tool-text">{m.text ?? ""}</span>
          {hasBlocks(m) && toolBlocks.value?.(m)}
        </div>
      );
    case "error":
      return (
        <div class="msg msg-error" data-i={m.i} dir="auto">
          {t("chat.error", { text: m.text ?? "" })}
        </div>
      );
    default: {
      const body = (
        <div class="msg-body">
          {m.md && !m.streaming ? <Md doc={m.md} /> : <MsgText text={m.text} streaming={m.streaming} />}
          {rest}
        </div>
      );
      return (
        <div class="msg msg-ai" data-i={m.i}>
          {look.value?.m(m, body) ?? (
            <>
              <div class="msg-label">
                <span class="star" aria-hidden="true">✦</span>
                <span>{t("chat.ai")}</span>
                {m.model && <span class="msg-model">{m.model}</span>}
              </div>
              {body}
              <Run m={m} />
            </>
          )}
          <Extras m={m} canFetch={canFetch} />
        </div>
      );
    }
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
  const desk = layout.value === "desktop";
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
        {look.value?.top(msgs)}
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
  if (link.meta.value?.role === "viewer") {
    // A person the chat was shared with comments instead (CMP-88).
    foot = <CommentBox chat={pin ? pin.id : laptop?.id ?? ""} />;
  } else if (pin) {
    // An old chat goes on here: the send opens it on the laptop, then the
    // page follows the laptop again (WEBP-10).
    foot = (
      <>
        <p class="composer-note chat-continue" dir="auto">
          {t("chat.continue")}
        </p>
        <Composer chat={pin.id} busy={laptop?.busy ?? false} onSent={follow} />
      </>
    );
  } else if (st) {
    foot = <Composer chat={laptop?.id ?? ""} busy={laptop?.busy ?? false} profile={laptop?.profile ?? ""} model={laptop?.model ?? ""} ctx={laptop?.ctx_pct ? { pct: laptop.ctx_pct, tokens: laptop.ctx_tokens ?? 0, window: laptop.ctx_window ?? 0 } : undefined} />;
  }
  return (
    <View
      tab="chat"
      title={t("tab.chat")}
      sub={chat?.title ? <span class="trunc" dir="auto">{chat.title}</span> : undefined}
      actions={
        <>
          {pin && <span class="chat-pinned">📌 {t("chat.pinned")}</span>}
          <button type="button" class={`head-btn chat-list-btn${list ? " is-on" : ""}`} aria-pressed={list ? "true" : "false"} title={desk ? t("chat.list") : undefined} onClick={() => setList(!list)}>
            {face(desk, "history", t("chat.list"))}
          </button>
          {!pin && link.meta.value?.role === "controller" && (
            <button
              type="button"
              class="head-btn chat-more"
              aria-label={t("scm.more_actions")}
              title={t("scm.more_actions")}
              onClick={(e) => openMore({ id: laptop?.id ?? "", title: laptop?.title ?? "", msgs: laptop?.messages ?? [] }, (e.currentTarget as HTMLElement).getBoundingClientRect())}
            >
              <Cod name="ellipsis" />
            </button>
          )}
          {pin ? (
            <button type="button" class="head-btn chat-follow" onClick={follow}>
              {t("chat.follow")}
            </button>
          ) : (
            <button type="button" class="head-btn chat-new" title={desk ? t("chat.new") : undefined} onClick={newChat}>
              {face(desk, "add", t("chat.new"))}
            </button>
          )}
        </>
      }
      bodyRef={body}
      foot={foot}
    >
      {content}
      <ChatMoreFlows />
    </View>
  );
}
