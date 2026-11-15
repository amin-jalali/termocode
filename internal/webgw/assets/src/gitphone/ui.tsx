// The parts every phone Git page shares: line icons, the long press
// (or right click) that opens a row's menu, and that menu: a small popover
// with the row's name on top, "Ask AI about this" and "Add to chat" first.
import type { ComponentChildren } from "preact";
import { H, h } from "./jsx";
import { s } from "./strings";

const PATHS: Record<string, string> = {
  branch: "M6 3v12M18 9a9 9 0 0 1-9 9",
  down: "M6 9l6 6 6-6",
  history: "M3 12a9 9 0 1 0 3-6.7L3 8M3 3v5h5M12 7v5l4 2",
  sync: "M7 4v16M7 4L3 8M7 4l4 4M17 20V4M17 20l-4-4M17 20l4-4",
  check: "M20 6L9 17l-5-5",
  gear: "M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z",
  spark: "M12 3l1.8 5.4L19 10l-5.2 1.6L12 17l-1.8-5.4L5 10l5.2-1.6z",
  link: "M10 13a5 5 0 0 0 7 0l3-3a5 5 0 0 0-7-7l-1 1M14 11a5 5 0 0 0-7 0l-3 3a5 5 0 0 0 7 7l1-1",
  plusminus: "M12 4v10M7 9h10M7 19h10",
  trash: "M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3",
  close: "M6 6l12 12M18 6L6 18",
  back: "M15 18l-6-6 6-6",
  search: "M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM21 21l-4.3-4.3",
  copy: "M9 9h11v11H9zM5 15H4V4h11v1",
  undo: "M3 7v6h6M3 13a9 9 0 1 0 3-7",
  plus: "M12 5v14M5 12h14",
  up: "M18 15l-6-6-6 6",
  share: "M12 3v12M8 7l4-4 4 4M5 13v7h14v-7",
  right: "M9 6l6 6-6 6",
  downarrow: "M12 5v14M6 13l6 6 6-6",
  uparrow: "M12 19V5M6 11l6-6 6 6",
};

/** Icon is a line icon (24 grid) at size px. */
export function Icon({ n, size = 20 }: { n: string; size?: number }) {
  return (
    <svg class="gp-ic" width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      {n === "branch" && <circle cx="18" cy="6" r="3" />}
      {n === "branch" && <circle cx="6" cy="18" r="3" />}
      {n === "gear" && <circle cx="12" cy="12" r="3" />}
      {n === "more" ? [5, 12, 19].map((x) => <circle key={x} cx={x} cy="12" r="1.3" fill="currentColor" />) : <path d={PATHS[n]} />}
    </svg>
  );
}

export const LONG_MS = 450;
const MOVE_PX = 10;

/** At is where a menu opens (the press, in the viewport). */
export interface At {
  x: number;
  y: number;
}

/** swallowTap drops what the lifted finger sends next (the mouse events
 * and the click a browser makes of a touch): the menu now lies under it,
 * and that must not pick or close it (lib/longpress.ts does the same). */
function swallowTap(node: HTMLElement, x: number, y: number): void {
  const end = (e: TouchEvent) => {
    e.preventDefault();
    setTimeout(off, 300);
  };
  const drop = (e: MouseEvent) => {
    if (Math.hypot(e.clientX - x, e.clientY - y) > MOVE_PX) return;
    e.stopPropagation();
    e.preventDefault();
    off();
  };
  const off = () => {
    node.removeEventListener("touchend", end);
    document.removeEventListener("click", drop, true);
  };
  node.addEventListener("touchend", end, { once: true, passive: false });
  document.addEventListener("click", drop, true);
}

/** hold gives an element the long press and the right click: onHold gets
 * the point; the click the lifted finger makes then does nothing. */
export function hold(onHold: (at: At) => void) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let from: At | null = null;
  let fired = false;
  const stop = () => {
    clearTimeout(timer);
    from = null;
  };
  return {
    onPointerDown: (e: PointerEvent) => {
      if (e.button > 0) return;
      fired = false;
      from = { x: e.clientX, y: e.clientY };
      const at = from;
      clearTimeout(timer);
      const node = e.currentTarget as HTMLElement;
      timer = setTimeout(() => {
        fired = true;
        from = null;
        swallowTap(node, at.x, at.y);
        H.haptic();
        onHold(at);
      }, LONG_MS);
    },
    onPointerMove: (e: PointerEvent) => {
      if (from && Math.hypot(e.clientX - from.x, e.clientY - from.y) > MOVE_PX) stop();
    },
    onPointerUp: stop,
    onPointerCancel: stop,
    onContextMenu: (e: MouseEvent) => {
      e.preventDefault();
      stop();
      if (!fired) onHold({ x: e.clientX, y: e.clientY });
      fired = true;
    },
    onClickCapture: (e: MouseEvent) => {
      if (!fired) return;
      fired = false;
      e.preventDefault();
      e.stopPropagation();
    },
  };
}

/** MenuItem is one entry of a row's menu. */
export interface MenuItem {
  label: string;
  icon: string;
  run: () => void;
  kind?: "ai" | "danger";
  hidden?: boolean;
}

/** Menu is a row's popover menu at the press: the row's name, then the
 * items. A tap outside or Esc closes it. */
export function Menu({ at, title, items, onClose }: { at: At; title: string; items: MenuItem[]; onClose: () => void }) {
  const ref = H.useRef<HTMLDivElement | null>(null);
  H.useEffect(() => {
    const el = ref.current;
    if (!el) return;
    // Keep it on the screen: under the press when it fits, else above.
    const r = el.getBoundingClientRect();
    const vw = innerWidth, vh = innerHeight;
    el.style.left = Math.max(8, Math.min(at.x - r.width / 2, vw - r.width - 8)) + "px";
    el.style.top = (at.y + 12 + r.height < vh - 8 ? at.y + 12 : Math.max(8, at.y - r.height - 12)) + "px";
    el.focus();
    const esc = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    document.addEventListener("keydown", esc);
    return () => document.removeEventListener("keydown", esc);
  }, []);
  return (
    <div class="gp-menu-layer">
      <button type="button" class="gp-scrim" aria-label={s("menu_close")} onClick={onClose} />
      <div class="gp-menu" role="menu" aria-label={title} ref={ref} tabIndex={-1}>
        <div class="gp-menu-t" dir="ltr">
          {title}
        </div>
        {items
          .filter((i) => !i.hidden)
          .map((i) => (
            <button
              type="button"
              role="menuitem"
              class={`gp-mi${i.kind ? " is-" + i.kind : ""}`}
              key={i.label}
              onClick={() => {
                onClose();
                i.run();
              }}
            >
              <Icon n={i.icon} size={18} />
              {i.label}
            </button>
          ))}
      </div>
    </div>
  );
}

/** chatItems are the two first entries of every menu: ask the AI about
 * the thing, or put it into the next message. */
export function chatItems(ref: Parameters<typeof H.addRef>[0], title: string): MenuItem[] {
  const add = (ask: boolean) => {
    const r = H.addRef(ref, title);
    if (r === "secret") return H.toast("warn", H.t("tochat.secret"));
    if (r === "full") return H.toast("warn", H.t("tochat.full"));
    if (r === "bad") return H.toast("warn", H.t("tochat.bad"));
    if (ask) H.goTab("chat");
    else H.toast("ok", s(r === "already" ? "already" : "added"));
  };
  return [
    { label: s("ask"), icon: "spark", kind: "ai", run: () => add(true) },
    { label: s("add_chat") + " ↩", icon: "link", run: () => add(false) },
  ];
}

/** Bar is a page's head: back (or nothing), the title, a small part after
 * it, and the page's buttons at the end. */
export function Btn({ icon, label, act, onClick, children }: { icon: string; label: string; act: string; onClick: (e: MouseEvent) => void; children?: ComponentChildren }) {
  return (
    <button type="button" class="gp-ib" data-act={act} aria-label={label} title={label} onClick={onClick}>
      <Icon n={icon} size={21} />
      {children}
    </button>
  );
}
