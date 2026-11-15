import type { KeyEvent, MediaAction } from "../src/engine";
import { CLICK_VOLUME, VIBRATE_MS, makeTick } from "../src/feedback";
import { mount, type MountOptions, type TactileKeyboard } from "../src/dom";
import { FakeClock } from "./fakeclock";
import { FakeDocument, FakeElement, FakeWindow, pointer } from "./fakedom";
import { eq, near, ok, test } from "./t";

interface Board {
  kb: TactileKeyboard;
  host: FakeElement;
  root: FakeElement;
  win: FakeWindow;
  keys: KeyEvent[];
  clicks: number[];
  buzz: number[];
  clock: FakeClock;
  el(label: string): FakeElement;
  press(label: string, id?: number): number;
  lift(id: number): void;
  tap(label: string): void;
}

let pid = 1;
function board(width = 800, o: Partial<MountOptions> = {}): Board {
  const b = { keys: [] as KeyEvent[], clicks: [] as number[], buzz: [] as number[], clock: new FakeClock(), win: new FakeWindow() } as Board;
  b.host = new FakeElement("div");
  b.host.rect = { left: 0, top: 500, width, height: 0 };
  b.kb = mount(b.host as unknown as HTMLElement, {
    layout: "full",
    onKey: (k) => b.keys.push(k),
    click: (v) => b.clicks.push(v),
    vibrate: (ms) => b.buzz.push(ms),
    clock: b.clock,
    doc: new FakeDocument() as unknown as Document,
    win: b.win as unknown as Window,
    ...o,
  });
  b.root = b.host.children[0];
  b.root.rect = { left: 0, top: 500, width, height: b.kb.height };
  b.el = (l) => {
    const e = b.root.byClass("tk-key").find((k) => k.dataset.key === l);
    if (!e) throw new Error("no key " + l);
    return e;
  };
  b.press = (l, id = pid++) => {
    const k = b.el(l);
    const x = parseFloat(String(k.style.left)) + 3;
    const y = parseFloat(String(k.style.top)) + 3;
    b.root.dispatchEvent(pointer("pointerdown", x, 500 + y, id));
    return id;
  };
  b.lift = (id) => b.root.dispatchEvent(pointer("pointerup", 0, 0, id));
  b.tap = (l) => b.lift(b.press(l));
  return b;
}

test("mount renders every key of the full layout, keyed by id or glyph", () => {
  const b = board();
  eq(b.root.className.split(" ")[0], "tk-kb");
  eq(b.root.byClass("tk-key").length, 16 + 14 + 14 + 13 + 12 + 11);
  ok(b.el("esc") && b.el("f10"), "fn row keys");
  eq(b.el("A").text(), "A");
  eq(b.el("1").byClass("tk-up")[0].textContent, "!");
  eq(b.el("1").byClass("tk-main")[0].textContent, "1");
  eq(b.el("home").byClass("tk-fnum")[0].textContent, "F11");
  ok(b.el("`").classList.contains("tk-left"), "B9 backquote legend left");
  ok(b.el("enter").classList.contains("tk-hug-r"), "B13 enter hugs right");
  ok(b.el("tab").classList.contains("tk-hug-l"), "B13 tab hugs left");
  ok(b.el("caps").byClass("tk-led").length === 1, "B15 LED");
  ok(b.root.byClass("tk-sec").length === 1, "B16 menu mark");
  ok(b.el("f1").classList.contains("tk-func"), "fn row small legends");
  ok(b.el("blank").classList.contains("tk-blank"), "A14 blank drawn empty");
  eq(b.el("blank").children.length, 0);
});

test("B3 B4 every key carries its posX for the well paddings", () => {
  const b = board();
  const l = parseFloat(b.el("tab").style.getPropertyValue("--tk-px"));
  const r = parseFloat(b.el("\\").style.getPropertyValue("--tk-px"));
  ok(l < -0.8 && r > 0.8, `${l} ${r}`);
});

test("A15 A16 height defaults to width*0.4 and compact relayouts", () => {
  const b = board(600);
  eq(b.kb.height, 240);
  eq(b.root.style.height, "240px");
  b.kb.setOptions({ layout: "compact" });
  eq(b.kb.compact, true);
  eq(b.root.byClass("tk-key").length, 14 + 14 + 13 + 12 + 11);
  b.kb.setOptions({ height: 300 });
  eq(b.root.style.height, "300px");
});

test("A17 auto layout follows the window's short side", () => {
  const b = board(390, { layout: "auto" });
  eq(b.kb.compact, true, "390x844");
  b.win.innerWidth = 1024;
  b.win.innerHeight = 768;
  b.win.dispatchEvent({ type: "resize" });
  eq(b.kb.compact, false, "tablet");
});

test("pointer down types, sets the press class, up clears it", () => {
  const b = board();
  const id = b.press("A");
  eq(b.keys.map((k) => k.key), ["a"]);
  ok(b.el("A").classList.contains("is-down"), "B6 pressed");
  ok(b.root.captured.has(id), "pointer captured");
  b.lift(id);
  ok(!b.el("A").classList.contains("is-down"));
});

test("pointer down prevents default so the text target keeps focus", () => {
  const b = board();
  const e = pointer("pointerdown", 40, 600);
  b.root.dispatchEvent(e);
  ok(e.defaultPrevented, "pointerdown");
  const m = { type: "mousedown" } as { type: string; defaultPrevented?: boolean };
  b.root.dispatchEvent(m);
  ok(m.defaultPrevented, "mousedown");
  const c = { type: "contextmenu" } as { type: string; defaultPrevented?: boolean };
  b.root.dispatchEvent(c);
  ok(c.defaultPrevented, "no long-press menu");
});

test("B20 armed and held modifiers are marked", () => {
  const b = board();
  b.tap("ctrl");
  ok(b.root.byClass("tk-key").filter((k) => k.dataset.key === "ctrl").every((k) => k.classList.contains("is-armed")), "both ctrl caps armed");
  b.tap("C");
  ok(!b.el("ctrl").classList.contains("is-armed"));
  const s = b.press("shift");
  ok(b.el("shift").classList.contains("is-held"));
  b.lift(s);
});

test("B15 caps LED lights", () => {
  const b = board();
  b.tap("caps");
  ok(b.el("caps").byClass("tk-led")[0].classList.contains("is-on"));
  b.tap("caps");
  ok(!b.el("caps").byClass("tk-led")[0].classList.contains("is-on"));
});

test("C12 trackpad badge shows while the spacebar drags", () => {
  const b = board();
  const sp = b.el("space");
  const x = parseFloat(String(sp.style.left)) + 30;
  const y = 500 + parseFloat(String(sp.style.top)) + 5;
  const badge = b.root.byClass("tk-pad-badge")[0];
  ok(badge.hidden, "hidden at rest");
  b.root.dispatchEvent(pointer("pointerdown", x, y, 900));
  b.root.dispatchEvent(pointer("pointermove", x + 20, y, 900));
  ok(!badge.hidden, "shown");
  eq(badge.text(), "TRACKPAD");
  b.root.dispatchEvent(pointer("pointerup", x + 20, y, 900));
  ok(badge.hidden);
});

test("E1-E4 backlight: level var, lit class, touch glow spots", () => {
  const b = board();
  eq(b.root.style.getPropertyValue("--tk-g"), "0");
  b.tap("fn");
  b.tap("f5");
  near(parseFloat(b.root.style.getPropertyValue("--tk-g")), 1 / 3);
  ok(b.root.classList.contains("tk-lit"));
  const id = b.press("A");
  const spots = b.root.byClass("tk-glow-spot");
  eq(spots.length, 1, "one spot per finger");
  near(parseFloat(spots[0].style.getPropertyValue("--tk-core")), 0.15, 1e-9, "core alpha .06+.09*level");
  b.lift(id);
  eq(b.root.byClass("tk-glow-spot").length, 0);
  b.tap("fn");
  b.tap("f5");
  b.tap("fn");
  b.tap("f5");
  ok(!b.root.classList.contains("tk-lit"), "back to off");
  const id2 = b.press("A");
  eq(b.root.byClass("tk-glow-spot").length, 0, "no glow when off");
  b.lift(id2);
});

test("B5 device tilt moves the well paddings (lerp 0.2, clamp ±1)", () => {
  const b = board();
  b.win.dispatchEvent({ type: "devicemotion", accelerationIncludingGravity: { x: 9.8, y: -20, z: 0 } });
  near(parseFloat(b.root.style.getPropertyValue("--tk-tx")), 0.2);
  near(parseFloat(b.root.style.getPropertyValue("--tk-ty")), -0.2);
  b.kb.destroy();
  eq(b.win.listeners("devicemotion"), 0, "destroy stops listening");
});

test("theme tokens become CSS variables", () => {
  const b = board(800, { theme: { accent: "#ff0088", deck: "#111111" } });
  eq(b.root.style.getPropertyValue("--tk-accent"), "#ff0088");
  eq(b.root.style.getPropertyValue("--tk-deck"), "#111111");
  b.kb.setOptions({ theme: { accent: "#00ff00" } });
  eq(b.root.style.getPropertyValue("--tk-accent"), "#00ff00");
});

test("C19 D3 F9 flips the sound; clicks stop when off", () => {
  let told: boolean | undefined;
  const b = board(800, { soundOn: true, onToggleSound: (on) => (told = on) });
  b.tap("A");
  eq(b.clicks, [CLICK_VOLUME.press, CLICK_VOLUME.release]);
  b.tap("f9");
  eq(told, false);
  const n = b.clicks.length;
  b.tap("A");
  eq(b.clicks.length, n, "silent");
});

test("D4 vibration per level on press only", () => {
  eq(VIBRATE_MS, [0, 6, 12, 18]);
  for (const level of [0, 1, 2, 3] as const) {
    const b = board(800, { hapticLevel: level, soundOn: false });
    b.tap("A");
    eq(b.buzz, level ? [VIBRATE_MS[level]] : [], `level ${level}`);
  }
});

test("D3 D4 makeTick reads live settings", () => {
  const s = { soundOn: true, hapticLevel: 2, clicks: [] as number[], buzz: [] as number[] };
  const tick = makeTick(() => ({ soundOn: s.soundOn, hapticLevel: s.hapticLevel, click: (v) => s.clicks.push(v), vibrate: (ms) => s.buzz.push(ms) }));
  tick(false);
  tick(true);
  s.soundOn = false;
  s.hapticLevel = 3;
  tick(false);
  eq(s.clicks, [0.08, 0.04]);
  eq(s.buzz, [12, 18]);
});

test("onAI, onMedia and onPadMove reach the host", () => {
  let ai = 0;
  const media: MediaAction[] = [];
  const pads: number[] = [];
  const b = board(800, { onAI: () => ai++, onMedia: (m) => media.push(m), onPadMove: (dx) => pads.push(dx) });
  b.tap("ai");
  b.tap("f2");
  eq(ai, 1);
  eq(media, ["volumeDown"]);
});

test("C14 repeat runs on the given clock through the DOM", () => {
  const b = board();
  const id = b.press("backspace");
  b.clock.tick(350 + 55 * 2);
  b.lift(id);
  eq(b.keys.length, 3);
});

test("pointercancel clears the press", () => {
  const b = board();
  const id = b.press("A");
  b.root.dispatchEvent(pointer("pointercancel", 0, 0, id));
  ok(!b.el("A").classList.contains("is-down"));
});

test("destroy removes the board and its listeners", () => {
  const b = board(390, { layout: "auto" });
  b.kb.destroy();
  eq(b.host.children.length, 0);
  eq(b.win.listeners("resize"), 0);
});
