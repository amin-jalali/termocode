// Numbered code rows of the file view (web UX 5.5). Each row is LTR and
// isolated, so a right-to-left word inside cannot reorder it (P7). A file
// over 2 000 lines is virtualised: rows draw in blocks of 100 near the
// screen, the rest are spacers of their measured (or guessed) height, and
// a "line N of M" chip shows while scrolling.
import { h } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { t } from "../strings";
import { CodeRow } from "./rows";

export const BLOCK = 100;
export const VIRTUAL_FROM = 2000;
const CHIP_MS = 800;

export interface Flash {
  /** id changes with every new change, so the flash plays again. */
  id: number;
  a: number;
  b: number;
}

export interface LinesProps {
  lines: readonly string[];
  /** scroller is the element that scrolls (the view body). */
  scroller: { current: HTMLElement | null };
  wrap: boolean;
  flash?: Flash | null;
}

export function Lines({ lines, scroller, wrap, flash }: LinesProps) {
  const n = lines.length;
  const blocks = Math.ceil(n / BLOCK);
  const virtual = n > VIRTUAL_FROM;
  const box = useRef<HTMLDivElement | null>(null);
  const heights = useRef(new Map<number, number>());
  const rowPx = useRef(20);
  const [win, setWin] = useState<[number, number]>([0, 3]);
  const [chip, setChip] = useState<number | null>(null);

  const rowsIn = (i: number) => Math.min(BLOCK, n - i * BLOCK);
  const est = (i: number) => heights.current.get(i) ?? rowsIn(i) * rowPx.current;

  // Which blocks are near the screen: one screen above, two below.
  useEffect(() => {
    if (!virtual) return;
    const sc = scroller.current;
    if (!sc) return;
    let raf = 0;
    let hide: ReturnType<typeof setTimeout> | undefined;
    let width = sc.clientWidth;
    const update = (scrolled: boolean) => {
      raf = 0;
      const el = box.current;
      if (!el) return;
      const rel = sc.getBoundingClientRect().top - el.getBoundingClientRect().top;
      const view = sc.clientHeight;
      let y = 0, a = -1, b = blocks - 1, line = 0;
      for (let i = 0; i < blocks; i++) {
        const hgt = est(i);
        if (rel >= y && rel < y + hgt) line = i * BLOCK + Math.floor(((rel - y) / hgt) * rowsIn(i));
        if (a < 0 && y + hgt > rel - view) a = i;
        if (y > rel + 2 * view) {
          b = i;
          break;
        }
        y += hgt;
      }
      setWin([Math.max(a, 0), b]);
      if (scrolled) {
        setChip(Math.min(Math.max(line, 0), n - 1) + 1);
        clearTimeout(hide);
        hide = setTimeout(() => setChip(null), CHIP_MS);
      }
    };
    const onScroll = () => {
      if (!raf) raf = requestAnimationFrame(() => update(true));
    };
    const ro = new ResizeObserver(() => {
      if (sc.clientWidth !== width) {
        width = sc.clientWidth;
        heights.current.clear(); // rows wrap differently now
      }
      if (!raf) raf = requestAnimationFrame(() => update(false));
    });
    sc.addEventListener("scroll", onScroll, { passive: true });
    ro.observe(sc);
    update(false);
    return () => {
      sc.removeEventListener("scroll", onScroll);
      ro.disconnect();
      cancelAnimationFrame(raf);
      clearTimeout(hide);
    };
  }, [virtual, blocks, n]);

  useEffect(() => heights.current.clear(), [wrap]);

  // Measure the drawn blocks, so their spacers keep the same height later.
  useLayoutEffect(() => {
    const el = box.current;
    if (!el || !virtual) return;
    let rows = 0, px = 0;
    el.querySelectorAll<HTMLElement>(".lines-block").forEach((b) => {
      const i = Number(b.dataset.block);
      heights.current.set(i, b.offsetHeight);
      rows += rowsIn(i);
      px += b.offsetHeight;
    });
    if (rows > 0) rowPx.current = px / rows;
  });

  const row = (i: number) => {
    const f = flash && i >= flash.a && i <= flash.b;
    return <CodeRow key={f ? `f${flash!.id}-${i}` : i} i={i} n={i + 1} text={lines[i]} flash={!!f} />;
  };
  const out = [];
  for (let k = 0; k < blocks; k++) {
    const shown = !virtual || (k >= win[0] && k <= win[1]);
    if (!shown) {
      out.push(<div key={"g" + k} class="lines-gap" data-block={k} style={{ height: est(k) + "px" }} />);
      continue;
    }
    const rows = [];
    for (let i = k * BLOCK; i < Math.min(n, (k + 1) * BLOCK); i++) rows.push(row(i));
    out.push(
      <div key={"b" + k} class="lines-block" data-block={k}>
        {rows}
      </div>,
    );
  }
  return (
    <div
      ref={box}
      class={`lines${wrap ? "" : " is-nowrap"}`}
      style={{ "--nw": String(n).length + 1 + "ch" }}
    >
      {chip !== null && (
        <span class="chip line-chip" role="status">
          {t("file.line_of", { n: chip, m: n })}
        </span>
      )}
      {out}
    </div>
  );
}
