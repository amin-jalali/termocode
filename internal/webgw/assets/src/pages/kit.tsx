// The phone tool pages' own small kit (pages.js): a section with its
// count and ⚙, a card of rows (icon tile, plain name, value, ›), a
// switch, chip filters and the press menu a long press opens (✦ Ask AI
// about this · Add to chat ↩ · the page's own items). Page-local on
// purpose: src/kit is shared, these shapes are the page sheets' (UI
// pages 10, 11), drawn from the same tokens.
import type { ComponentChildren } from "preact";
import type { Signal } from "@preact/signals";
import type { Attach } from "../../../../webproto/proto";
import { h, H } from "./host";
import { tx } from "./strings";

/** P are the icons' paths (24×24, stroked). */
export const P = {
  back: "M15 18l-6-6 6-6",
  chev: "M9 18l6-6-6-6",
  down: "M6 9l6 6 6-6",
  search: "M11 4a7 7 0 1 0 0 14a7 7 0 1 0 0-14M21 21l-4.3-4.3",
  gear: "M12 9a3 3 0 1 0 0 6a3 3 0 1 0 0-6M19.4 15l1.6 1.2-2 3.4-1.9-.8a7 7 0 0 1-2 1.2L14.8 22h-4l-.3-2a7 7 0 0 1-2-1.2l-1.9.8-2-3.4L6.2 15a7 7 0 0 1 0-2.4L4.6 11.4l2-3.4 1.9.8a7 7 0 0 1 2-1.2l.3-2h4l.3 2a7 7 0 0 1 2 1.2l1.9-.8 2 3.4-1.6 1.2a7 7 0 0 1 0 2.4z",
  check: "M5 12l5 5 9-10",
  plus: "M12 5v14M5 12h14",
  palette: "M12 3a9 9 0 1 0 0 18a9 9 0 1 0 0-18M8 9.5h.01M12 7h.01M16 9.5h.01",
  type: "M4 7V4h16v3M9 20h6M12 4v16",
  bolt: "M13 2L4 14h7l-1 8 9-12h-7z",
  eye: "M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7S2 12 2 12zM12 9a3 3 0 1 0 0 6a3 3 0 1 0 0-6",
  eyeOff: "M3 3l18 18M10.6 6.1A10 10 0 0 1 12 6c6.4 0 10 6 10 6a17 17 0 0 1-3.2 3.8M6.6 6.6A17 17 0 0 0 2 12s3.6 6 10 6a9.7 9.7 0 0 0 4.4-1",
  file: "M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8zM14 2v6h6",
  wrap: "M4 6h16M8 12h12M8 18h12",
  list: "M8 6h13M8 12h13M8 18h13M3.5 6h.01M3.5 12h.01M3.5 18h.01",
  spark: "M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z",
  plug: "M9 2v6M15 2v6M6 8h12v3a6 6 0 0 1-12 0zM12 17v5",
  globe: "M12 3a9 9 0 1 0 0 18a9 9 0 1 0 0-18M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18",
  term: "M4 17l6-5-6-5M12 19h8",
  refresh: "M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7",
  bug: "M8 6a4 4 0 0 1 8 0M6 10h12v4a6 6 0 0 1-12 0zM3 13h3M18 13h3M4 7l3 3M20 7l-3 3M12 10v10",
  play: "M7 4l13 8-13 8z",
  link: "M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1",
  copy: "M9 9h11v11H9zM5 15H4V4h11v1",
  more: "M5 12h.01M12 12h.01M19 12h.01",
  stepOver: "M4 15a8 8 0 0 1 15-3M19 6v6h-6M12 19h.01",
  stepInto: "M12 4v11M7 10l5 5 5-5M12 20h.01",
  stepOut: "M12 16V5M7 10l5-5 5 5M12 20h.01",
  restart: "M4 12a8 8 0 1 0 2.3-5.7M4 4v5h5",
  stop: "M6 6h12v12H6z",
  pause: "M8 5v14M16 5v14",
  watch: "M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7S2 12 2 12zM12 9a3 3 0 1 0 0 6a3 3 0 1 0 0-6",
};

export type IconName = keyof typeof P;

/** Ic is one stroked icon. */
export function Ic({ n, size = 18 }: { n: IconName; size?: number }) {
  return (
    <svg class="pg-ic" width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d={P[n]} />
    </svg>
  );
}

/** Section is one group of the page: its title, a count, ⚙ and a card. */
export function Section({ title, count, tool, children, id }: { title: string; count?: number; tool?: ComponentChildren; children: ComponentChildren; id: string }) {
  return (
    <section class="pg-sec" data-sec={id} aria-labelledby={`pgs-${id}`}>
      <div class="pg-sec-h">
        <h2 id={`pgs-${id}`}>{title}</h2>
        {count !== undefined && <span class="n">{count}</span>}
        {tool}
      </div>
      <div class="pg-card">{children}</div>
    </section>
  );
}

/** Switch is a row's on/off look (the row is the button). */
export const Switch = ({ on }: { on: boolean }) => <span class={`pg-sw${on ? " on" : ""}`} aria-hidden="true" />;

/** press gives a row its long press (the context menu event: a long
 * press on a phone, a right click elsewhere). */
export const press = (open: (x: number, y: number) => void) => (e: MouseEvent) => {
  e.preventDefault();
  open(e.clientX, e.clientY);
};

export interface MenuItem {
  id: string;
  label: string;
  icon?: IconName;
  ai?: boolean;
  run: () => void;
}

interface OpenMenu {
  x: number;
  y: number;
  title: string;
  items: MenuItem[];
}

let box: Signal<OpenMenu | null> | undefined;
const menu = (): Signal<OpenMenu | null> => (box ??= H.signal<OpenMenu | null>(null));

/** openMenu opens the press menu at x, y. */
export function openMenu(x: number, y: number, title: string, items: MenuItem[]): void {
  menu().value = { x, y, title, items };
}

/** PressMenu draws the open press menu; each page mounts it once. A tap
 * outside or Esc closes it. */
export function PressMenu() {
  const m = menu().value;
  H.useEffect(() => {
    if (!m) return;
    const esc = (e: KeyboardEvent) => e.key === "Escape" && (menu().value = null);
    document.addEventListener("keydown", esc);
    (document.querySelector(".pg-menu") as HTMLElement | null)?.focus({ preventScroll: true });
    return () => document.removeEventListener("keydown", esc);
  }, [m]);
  if (!m) return null;
  const w = 240, hgt = 44 * m.items.length + 34;
  const left = Math.max(12, Math.min(m.x - w / 2, innerWidth - w - 12));
  const top = Math.max(12, Math.min(m.y + 8, innerHeight - hgt - 64));
  return (
    <div class="pg-menu-layer" onClick={() => (menu().value = null)}>
      <div class="pg-menu" role="menu" tabIndex={-1} aria-label={m.title} style={{ left: left + "px", top: top + "px", width: w + "px" }} onClick={(e) => e.stopPropagation()}>
        <div class="mt">{m.title}</div>
        {m.items.map((it) => (
          <button type="button" role="menuitem" key={it.id} data-act={it.id} class={it.ai ? "ai" : undefined} onClick={() => ((menu().value = null), it.run())}>
            {it.icon && <Ic n={it.icon} />}
            {it.label}
          </button>
        ))}
      </div>
    </div>
  );
}

/** useChat keeps the laptop's chat known, so a row can go to it. */
export const useChat = () => H.useEffect(() => H.link.subscribe("chat"), []);

/** toChat puts the item into the chat's next message (an attach chip). */
export function toChat(a: Attach, quiet = false): boolean {
  const id = H.chatID();
  if (!id || !H.addAttach(id, a)) {
    H.toast("warn", tx(id ? "pg.chat_full" : "pg.chat_none"));
    return false;
  }
  if (!quiet) H.toast("ok", tx("pg.chat_added"));
  return true;
}

/** askAI attaches the item, puts the question in the chat box (nothing
 * is sent) and opens Chat. */
export function askAI(a: Attach, text: string): void {
  if (!toChat(a, true)) return;
  H.pendingDraft.value = { text, notChat: "" };
  H.go({ tab: "chat", rest: "", query: {} });
}

/** aiItems are the two AI entries every press menu starts with. */
export function aiItems(a: Attach, ask: string): MenuItem[] {
  return [
    { id: "ask", label: tx("pg.ask"), icon: "spark", ai: true, run: () => askAI(a, ask) },
    { id: "chat", label: tx("pg.add_chat"), icon: "link", run: () => void toChat(a) },
  ];
}

/** viewAttach is a view ref: the laptop runs the spec and gives the
 * model its rows (webproto Attach kind view). */
export const viewAttach = (view: string, label: string, spec: object): Attach => ({ kind: "view", id: view, label: label.slice(0, 120), spec: JSON.stringify({ view, ...spec }) });

/** copy puts text on the clipboard and says so. */
export function copy(text: string): void {
  navigator.clipboard?.writeText(text).then(
    () => H.toast("ok", tx("pg.copied")),
    () => H.toast("warn", tx("pg.copy_failed")),
  );
}
