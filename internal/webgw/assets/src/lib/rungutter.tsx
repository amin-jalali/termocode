// The desktop editor's glyph margin (WEBP-4): the laptop's breakpoints
// and the tests of the file, and the row the debugger stopped on. A
// click left of a row number sets or removes a breakpoint (on a test's
// row it runs the test); a right click offers run and debug of the test
// and the kinds of breakpoints. A conditional breakpoint or a logpoint
// asks its text in a small box over the editor.
import { h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useRef } from "preact/hooks";
import type { DebugSnap, TestItem } from "../../../../webproto/proto";
import type { FrameGlyph, GlyphKind } from "../monaco/protocol";
import { link, store } from "../session";
import { readDebugSnap, readTestsSnap } from "../state/run";
import { t } from "../strings";
import { openMenu, type MenuItems } from "../ui/ContextMenu";
import { ask, canAct } from "../views/run/bits";

/** fileGlyphs are the marks of path: breakpoints over tests. */
export function fileGlyphs(path: string, tests: readonly TestItem[], dbg: DebugSnap | null): { items: FrameGlyph[]; stop: number } {
  const byLine = new Map<number, FrameGlyph>();
  for (const it of tests) {
    if (it.kind === "case" && it.path === path && it.line) byLine.set(it.line, { line: it.line, kind: ("test-" + it.state) as GlyphKind, title: it.label });
  }
  for (const b of dbg?.breakpoints ?? []) {
    if (b.path !== path) continue;
    const kind: GlyphKind = b.verified === "no" ? "bp-off" : b.log ? "bp-log" : b.condition || b.hit ? "bp-cond" : "bp";
    byLine.set(b.line, { line: b.line, kind, title: b.condition || b.log || undefined });
  }
  const top = dbg?.stopped ? dbg.frames.find((f) => f.id === dbg.frame) ?? dbg.frames[0] : undefined;
  return { items: [...byLine.values()], stop: top && top.path === path ? top.line : 0 };
}

/** bpAsk is the open box for a breakpoint's condition or log message. */
const bpAsk = signal<{ path: string; line: number; kind: "condition" | "log"; x: number; y: number } | null>(null);

/** onGlyph runs a click in the glyph margin of path's editor. */
export function onGlyph(path: string, line: number, menu: boolean, x: number, y: number): void {
  if (!canAct()) return;
  const tests = readTestsSnap(store.topic("tests").peek()?.data)?.items ?? [];
  const dbg = readDebugSnap(store.topic("debug").peek()?.data);
  const test = tests.find((it) => it.kind === "case" && it.path === path && it.line === line);
  const bp = dbg?.breakpoints.find((b) => b.path === path && b.line === line);
  const toggle = () => ask("debug.bp", { op: "toggle", path, line });
  if (!menu) {
    if (test && !bp) ask("tests.run", { id: test.id });
    else toggle();
    return;
  }
  const items: (MenuItems[number])[] = [];
  if (test) {
    items.push({ label: t("test.run"), run: () => ask("tests.run", { id: test.id }) });
    if (test.debug) items.push({ label: t("test.debug"), run: () => ask("tests.debug", { id: test.id }) });
    items.push(null);
  }
  if (bp) items.push({ label: t("dbg.bp_remove"), run: toggle });
  else items.push({ label: t("dbg.bp_add"), run: toggle });
  items.push({ label: t("dbg.bp_cond"), run: () => (bpAsk.value = { path, line, kind: "condition", x, y }) });
  items.push({ label: t("dbg.bp_logpt"), run: () => (bpAsk.value = { path, line, kind: "log", x, y }) });
  openMenu(x, y, t("dbg.bps"), items);
}

/** useRunGutter keeps an editor's glyph margin in step with the laptop. */
export function useRunGutter(path: string, on: boolean, set: (items: FrameGlyph[], stop: number) => void): void {
  useEffect(() => {
    if (on) link.subscribe("tests", "debug");
  }, [on]);
  const tests = store.topic("tests").value?.data;
  const dbg = store.topic("debug").value?.data;
  useEffect(() => {
    if (!on) return;
    const g = fileGlyphs(path, readTestsSnap(tests)?.items ?? [], readDebugSnap(dbg));
    set(g.items, g.stop);
  }, [on, path, tests, dbg]);
}

/** BpAsk is the box that asks a breakpoint's condition or log message. */
export function BpAsk({ path }: { path: string }) {
  const a = bpAsk.value;
  const box = useRef<HTMLInputElement | null>(null);
  useEffect(() => box.current?.focus(), [a]);
  if (!a || a.path !== path) return null;
  const close = () => (bpAsk.value = null);
  return (
    <div class="bp-ask" style={{ left: a.x + "px", top: a.y + "px" }}>
      <span class="bp-ask-line">{t("dbg.bp_at", { n: a.line })}</span>
      <input
        ref={box}
        class="bp-ask-input"
        dir="ltr"
        aria-label={a.kind === "condition" ? t("dbg.cond_ph") : t("dbg.log_ph")}
        placeholder={a.kind === "condition" ? t("dbg.cond_ph") : t("dbg.log_ph")}
        onKeyDown={(e) => {
          const v = (e.currentTarget as HTMLInputElement).value.trim();
          if (e.key === "Escape") close();
          if (e.key === "Enter" && v) {
            close();
            ask("debug.bp", { op: "set", path: a.path, line: a.line, [a.kind]: v });
          }
        }}
        onBlur={close}
      />
    </div>
  );
}
