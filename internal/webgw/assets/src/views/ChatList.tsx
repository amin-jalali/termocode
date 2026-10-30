// The chat list (web UX 5.2): the stored chats, newest first (at most
// 200), the laptop's own marked, a live dot on the one the AI answers in,
// and a filter when the list is long.
import { h } from "preact";
import { useState } from "preact/hooks";
import type { ChatItem } from "../../../../webproto/proto";
import { store } from "../session";
import { readChats } from "../state/chat";
import { t } from "../strings";
import { SkeletonRows } from "../ui/Skeleton";

const FILTER_FROM = 10;

function pad(n: number): string {
  return n < 10 ? "0" + n : String(n);
}

/** when writes a chat's time: the time today, else the date. */
export function when(ms: number, now = Date.now()): string {
  if (!ms) return "";
  const d = new Date(ms), n = new Date(now);
  if (d.toDateString() === n.toDateString()) return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export function ChatList({ current, onPick }: { current: string; onPick: (c: ChatItem) => void }) {
  const [q, setQ] = useState("");
  const st = store.topic("chats").value;
  if (!st) return <SkeletonRows rows={6} mono={false} />;
  const all = readChats(st.data);
  if (all.length === 0) return <p class="empty">{t("chat.list_empty")}</p>;
  const needle = q.trim().toLowerCase();
  const items = needle ? all.filter((c) => (c.title ?? "").toLowerCase().includes(needle)) : all;
  return (
    <div class="chat-list">
      {all.length > FILTER_FROM && (
        <input
          class="chat-filter"
          type="search"
          dir="auto"
          value={q}
          placeholder={t("chat.filter")}
          aria-label={t("chat.filter")}
          onInput={(e) => setQ((e.currentTarget as HTMLInputElement).value)}
        />
      )}
      {items.map((c) => (
        <button key={c.id} type="button" class={`chat-row${c.id === current ? " is-current" : ""}`} onClick={() => onPick(c)}>
          <span class="chat-row-title" dir="auto">
            {c.title || t("chat.untitled")}
          </span>
          <span class="chat-row-meta" dir="ltr">
            {c.live && <span class="live-dot" aria-hidden="true" />}
            <span>{when(c.updated)}</span>
            {c.open && <span class="on-laptop">{t("chat.on_laptop")}</span>}
          </span>
        </button>
      ))}
    </div>
  );
}
