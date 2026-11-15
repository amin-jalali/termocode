import { mountDock, threeFingerToggle, type Dock, type DockOptions } from "../src/dock";
import { SuggestionBar } from "../src/suggestions";
import { FakeClock } from "./fakeclock";
import { FakeDocument, FakeElement, FakeWindow, pointer } from "./fakedom";
import { eq, near, ok, test } from "./t";

interface Rig {
  dock: Dock;
  host: FakeElement;
  win: FakeWindow;
  target: FakeElement;
  sys: number;
  collapses: boolean[];
}

function rig(o: Partial<DockOptions> = {}, vh = 844, w = 390): Rig {
  const r = { win: new FakeWindow(), host: new FakeElement("div"), target: new FakeElement("textarea"), sys: 0, collapses: [] as boolean[] } as Rig;
  r.host.rect = { left: 0, top: 0, width: w, height: 0 };
  r.win.innerWidth = w;
  r.win.innerHeight = vh;
  r.dock = mountDock(r.host as unknown as HTMLElement, {
    onKey: () => undefined,
    doc: new FakeDocument() as unknown as Document,
    win: r.win as unknown as Window,
    clock: new FakeClock(),
    click: () => undefined,
    vibrate: () => undefined,
    viewport: () => vh,
    onSystemKeyboard: () => r.sys++,
    onCollapse: (c) => r.collapses.push(c),
    ...o,
  });
  return r;
}
const t = (r: Rig): HTMLElement => r.target as unknown as HTMLElement;
const part = (r: Rig, c: string): FakeElement => r.host.byClass(c)[0];

test("F1 the dock shows on attach and hides on detach of the same target", () => {
  const r = rig();
  const root = part(r, "tk-dock");
  ok(root.hidden, "hidden with no surface");
  r.dock.attach(t(r));
  ok(!root.hidden);
  r.dock.detach(new FakeElement("div") as unknown as HTMLElement);
  ok(!root.hidden, "another surface's blur does not hide it");
  r.dock.detach(t(r));
  ok(root.hidden);
});

test("F2 F3 height 380 by default, clamp 220..540, capped by the viewport", () => {
  const r = rig({}, 844, 844);
  r.dock.attach(t(r));
  eq(r.dock.kbHeight, 380);
  eq(r.dock.keyboard.height, 380);
  r.dock.setHeight(100);
  eq(r.dock.kbHeight, 220);
  r.dock.setHeight(900);
  eq(r.dock.kbHeight, 540);
  const land = rig({}, 390);
  land.dock.attach(t(land));
  eq(land.dock.kbHeight, 390 - 140 - 26, "L3 landscape phone leaves 140px");
  const tiny = rig({}, 200);
  tiny.dock.attach(t(tiny));
  eq(tiny.dock.kbHeight, 140, "never below 140");
});

// w / h of the A key on the dock's board.
const aspectA = (r: Rig): number => {
  const a = r.dock.keyboard.engine.placed.find((k) => k.def.main === "A")!;
  return a.w / a.h;
};

test("F2b a phone held upright: the keys look like the landscape ones, scaled down", () => {
  const land = rig({}, 390, 844);
  land.dock.attach(t(land));
  eq(land.dock.kbHeight, 390 - 140 - 26, "landscape keeps its height");
  const la = aspectA(land);
  near(la, 1.334, 0.01, "landscape A key");
  ok(!part(land, "tk-kb").classList.contains("tk-narrow"), "landscape is not narrow");
  const up = rig({}, 664, 390);
  up.dock.attach(t(up));
  const ua = aspectA(up);
  ok(ua >= la / 2 && ua <= la, `upright A key ${ua.toFixed(2)} wide per high, want ${(la / 2).toFixed(2)}..${la.toFixed(2)}`);
  ok(up.dock.kbHeight < 380 && up.dock.kbHeight >= 160, `upright board ${up.dock.kbHeight}`);
  eq(up.dock.keyboard.height, up.dock.kbHeight);
  ok(part(up, "tk-kb").classList.contains("tk-narrow"), "the upright board is narrow (labels fit one line)");
  const tall = rig({}, 844, 390);
  tall.dock.attach(t(tall));
  eq(tall.dock.kbHeight, up.dock.kbHeight, "the same keys on a taller phone");
  const set = rig({ height: 300 }, 664, 390);
  set.dock.attach(t(set));
  eq(set.dock.kbHeight, 300, "a height the host asks for is kept");
  up.dock.setHeight(260);
  eq(up.dock.kbHeight, 260, "and one set later");
});

test("F4 F5 F6 handle hides; collapsed bar shows it again", () => {
  const r = rig();
  r.dock.attach(t(r));
  ok(part(r, "tk-grab"), "grab bar");
  part(r, "tk-hide").dispatchEvent({ type: "click" });
  ok(r.dock.collapsed);
  eq(r.collapses, [true]);
  const bar = part(r, "tk-collapsed");
  ok(!bar.hidden && part(r, "tk-dock-body").hidden);
  eq(bar.text().trim(), "Show keyboard");
  bar.dispatchEvent({ type: "click" });
  ok(!r.dock.collapsed && bar.hidden);
});

test("F12 attaching re-expands a collapsed dock", () => {
  const r = rig();
  r.dock.attach(t(r));
  r.dock.toggleCollapsed();
  r.dock.detach(t(r));
  r.dock.attach(t(r));
  ok(!r.dock.collapsed);
});

test("F7 F8 dragging the handle shrinks the dock; release springs back", () => {
  const r = rig();
  r.dock.attach(t(r));
  const root = part(r, "tk-dock");
  const handle = part(r, "tk-handle");
  handle.dispatchEvent(pointer("pointerdown", 100, 400, 3));
  handle.dispatchEvent(pointer("pointermove", 100, 500, 3));
  eq(r.dock.dragY, 100, "1:1");
  eq(root.style.height, `${26 + 380 - 100}px`);
  handle.dispatchEvent(pointer("pointermove", 100, 2000, 3));
  eq(r.dock.dragY, 380 + 100, "clamped to kb + 100");
  handle.dispatchEvent(pointer("pointerup", 100, 2000, 3));
  r.win.frame(120);
  near(r.dock.dragY, 0, 1e-9, "settled home");
  ok(String(root.style.height).includes(`${26 + 380}px`));
});

test("F8 the spring is underdamped like the original (bounces)", () => {
  const r = rig();
  r.dock.attach(t(r));
  r.dock.dragBy(200);
  r.dock.dragEnd(0);
  let min = 1e9;
  for (let i = 0; i < 60; i++) {
    r.win.frame();
    min = Math.min(min, r.dock.dragY);
  }
  ok(min < 0 || min === 0, "it reaches home");
  eq(r.dock.dragY, 0);
});

test("F13 the system keyboard button hands over", () => {
  const r = rig();
  r.dock.attach(t(r));
  part(r, "tk-system").dispatchEvent({ type: "click" });
  eq(r.sys, 1);
});

test("F10 pressing the dock never moves the focus", () => {
  const r = rig();
  r.dock.attach(t(r));
  const e = { type: "mousedown" } as { type: string; defaultPrevented?: boolean };
  part(r, "tk-dock").dispatchEvent(e);
  ok(e.defaultPrevented);
  const p = pointer("pointerdown", 1, 1);
  part(r, "tk-collapsed").dispatchEvent(p);
  ok(p.defaultPrevented);
});

test("G1 G4 the suggestion strip takes 40px only with items", () => {
  const r = rig();
  r.dock.attach(t(r));
  const s = part(r, "tk-sugg");
  ok(s.hidden);
  eq(r.dock.kbHeight, 380);
  r.dock.suggestions.show([{ label: "printLine", insertText: "printLine()", detail: "fn" }], () => undefined);
  ok(!s.hidden);
  eq(part(r, "tk-dock").style.height, `calc(${26 + 40 + 380}px + env(safe-area-inset-bottom, 0px))`);
  const land = rig({}, 390);
  land.dock.attach(t(land));
  land.dock.suggestions.show([{ label: "x", insertText: "x", detail: "" }], () => undefined);
  eq(land.dock.kbHeight, 390 - 140 - 26 - 40);
});

test("G2 G3 chips render label, detail and accept then clear (KB-04)", () => {
  const bar = new SuggestionBar(new FakeDocument() as unknown as Document);
  const el = bar.el as unknown as FakeElement;
  let got = "";
  bar.show(
    [
      { label: "printLine", insertText: "printLine()", detail: "fn" },
      { label: "process", insertText: "process", detail: "" },
    ],
    (i) => (got = i.insertText),
  );
  const chips = el.byClass("tk-chip");
  eq(chips.length, 2);
  eq(chips[0].byClass("tk-chip-label")[0].textContent, "printLine");
  eq(chips[0].byClass("tk-chip-detail")[0].textContent, "fn");
  eq(chips[1].byClass("tk-chip-detail").length, 0, "no empty detail");
  chips[0].dispatchEvent({ type: "click" });
  eq(got, "printLine()");
  eq(bar.items.length, 0);
  ok(el.hidden);
  eq(el.byClass("tk-chip").length, 0);
});

test("L1 a hardware key on the surface collapses the dock", () => {
  const r = rig();
  r.dock.attach(t(r));
  r.target.dispatchEvent({ type: "keydown", key: "Unidentified", isTrusted: true });
  ok(!r.dock.collapsed, "IME placeholder keys are not a keyboard");
  r.target.dispatchEvent({ type: "keydown", key: "a", isTrusted: false });
  ok(!r.dock.collapsed, "synthetic keys are ignored");
  r.target.dispatchEvent({ type: "keydown", key: "a", isTrusted: true });
  ok(r.dock.collapsed);
  r.dock.detach(t(r));
  eq(r.target.listeners("keydown"), 0);
});

test("F11 a three-finger tap toggles the dock", () => {
  const r = rig();
  r.dock.attach(t(r));
  const canvas = new FakeElement("div");
  const off = threeFingerToggle(canvas as unknown as HTMLElement, r.dock);
  canvas.dispatchEvent({ type: "touchstart", touches: { length: 2 } });
  ok(!r.dock.collapsed);
  canvas.dispatchEvent({ type: "touchstart", touches: { length: 3 } });
  canvas.dispatchEvent({ type: "touchstart", touches: { length: 4 } });
  ok(r.dock.collapsed, "once per gesture");
  canvas.dispatchEvent({ type: "touchend", touches: { length: 0 } });
  canvas.dispatchEvent({ type: "touchstart", touches: { length: 3 } });
  ok(!r.dock.collapsed);
  off();
  eq(canvas.listeners("touchstart"), 0);
});

test("F9 a bottom safe-area filler", () => {
  const r = rig();
  r.dock.attach(t(r));
  ok(part(r, "tk-inset"));
});

test("setOptions reaches the keyboard; destroy removes everything", () => {
  const r = rig();
  r.dock.attach(t(r));
  r.dock.setOptions({ layout: "full" });
  eq(r.dock.keyboard.compact, false);
  r.dock.destroy();
  eq(r.host.children.length, 0);
});
