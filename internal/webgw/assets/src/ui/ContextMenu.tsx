// The desktop's context menus (right click, or a long press on a touch
// screen): a small list at the pointer, as the laptop's editor has them.
// Arrows move, Enter or a click runs, Esc, a click elsewhere or leaving
// the window close it, and the focus goes back where it was. Risky items
// never act here: they open the same questions the buttons do (Delete
// asks the laptop). The phone keeps its sheets.
import { h } from "preact";
import { useLayoutEffect, useRef, useState } from "preact/hooks";
import { signal } from "@preact/signals";

export interface MenuEntry {
  label: string;
  /** keys is the shortcut shown on the right ("Ctrl+W"). */
  keys?: string;
  disabled?: boolean;
  run: () => void;
}

/** MenuItems: entries, null for a separator line. */
export type MenuItems = readonly (MenuEntry | null)[];

interface Open {
  x: number;
  y: number;
  label: string;
  items: MenuItems;
  back: HTMLElement | null;
}

/** menu is the open context menu, or null. */
export const menu = signal<Open | null>(null);

/** openMenu shows items at the point (x, y) of the page. */
export function openMenu(x: number, y: number, label: string, items: MenuItems): void {
  const a = document.activeElement;
  menu.value = { x, y, label, items, back: a instanceof HTMLElement && a !== document.body ? a : null };
}

/** onMenu is an onContextMenu handler that opens items at the pointer. */
export function onMenu(label: string, items: () => MenuItems): (e: MouseEvent) => void {
  return (e: MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    openMenu(e.clientX, e.clientY, label, items());
  };
}

function closeMenu(refocus: boolean): void {
  const m = menu.peek();
  menu.value = null;
  if (refocus && m?.back?.isConnected) m.back.focus();
}

const EDGE = 4;

export function ContextMenu() {
  const m = menu.value;
  const box = useRef<HTMLDivElement | null>(null);
  const [at, setAt] = useState<{ left: number; top: number } | null>(null);
  useLayoutEffect(() => {
    const el = box.current;
    if (!m || !el) {
      setAt(null);
      return;
    }
    const r = el.getBoundingClientRect();
    setAt({
      left: Math.max(EDGE, Math.min(m.x, innerWidth - r.width - EDGE)),
      top: Math.max(EDGE, m.y + r.height > innerHeight - EDGE ? m.y - r.height : m.y),
    });
    const away = () => closeMenu(false);
    addEventListener("blur", away);
    addEventListener("resize", away);
    return () => {
      removeEventListener("blur", away);
      removeEventListener("resize", away);
    };
  }, [m]);
  // Once placed (it is hidden until then), the first item takes the focus.
  useLayoutEffect(() => {
    const el = box.current;
    if (at && el && !el.contains(document.activeElement)) el.querySelector<HTMLElement>(".ctx-item:not([disabled])")?.focus();
  }, [at]);
  if (!m) return null;
  const move = (dir: 1 | -1 | "first" | "last") => {
    const list = [...(box.current?.querySelectorAll<HTMLElement>(".ctx-item:not([disabled])") ?? [])];
    if (!list.length) return;
    const i = list.indexOf(document.activeElement as HTMLElement);
    const n = dir === "first" ? 0 : dir === "last" ? list.length - 1 : (i + dir + list.length) % list.length;
    list[n].focus();
  };
  return (
    <div
      class="ctx-layer"
      onPointerDown={(e) => e.target === e.currentTarget && closeMenu(false)}
      onContextMenu={(e) => {
        e.preventDefault();
        if (e.target === e.currentTarget) closeMenu(false);
      }}
    >
      <div
        ref={box}
        class="ctx-menu"
        role="menu"
        aria-label={m.label}
        style={at ? { left: at.left + "px", top: at.top + "px" } : { left: m.x + "px", top: m.y + "px", visibility: "hidden" }}
        onKeyDown={(e) => {
          const k = e.key;
          if (k === "Escape" || k === "Tab") {
            e.preventDefault();
            closeMenu(true);
          } else if (k === "ArrowDown" || k === "ArrowUp" || k === "Home" || k === "End") {
            e.preventDefault();
            move(k === "ArrowDown" ? 1 : k === "ArrowUp" ? -1 : k === "Home" ? "first" : "last");
          }
        }}
      >
        {m.items.map((it, i) =>
          it ? (
            <button
              key={i}
              type="button"
              role="menuitem"
              class="ctx-item"
              disabled={it.disabled}
              onClick={() => {
                closeMenu(true);
                it.run();
              }}
            >
              <span class="ctx-label">{it.label}</span>
              {it.keys && <span class="ctx-keys">{it.keys}</span>}
            </button>
          ) : (
            <div key={i} class="ctx-sep" role="separator" />
          ),
        )}
      </div>
    </div>
  );
}
