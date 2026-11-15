import { Engine, type EngineOptions, type KeyEvent, type MediaAction } from "../src/engine";
import type { Placed } from "../src/layout";
import { FakeClock } from "./fakeclock";
import { eq, near, ok, test } from "./t";

interface Rig {
  e: Engine;
  keys: KeyEvent[];
  ticks: boolean[];
  clock: FakeClock;
  ai: number;
  pads: [number, number][];
  media: MediaAction[];
  sound: number;
  typed(): string;
  find(f: (p: Placed) => boolean): number;
  idx(label: string): number;
  down(label: string, id?: number): number;
  up(id: number): void;
  tap(label: string): void;
}

let pid = 100;
function rig(compact = false, extra: Partial<EngineOptions> = {}): Rig {
  const r = { keys: [] as KeyEvent[], ticks: [] as boolean[], clock: new FakeClock(), ai: 0, pads: [] as [number, number][], media: [] as MediaAction[], sound: 0 } as Rig;
  r.e = new Engine({
    compact,
    clock: r.clock,
    onKey: (k) => r.keys.push(k),
    onAI: () => r.ai++,
    onPadMove: (dx, dy) => r.pads.push([dx, dy]),
    onMedia: (m) => r.media.push(m),
    onToggleSound: () => r.sound++,
    tick: (rel) => r.ticks.push(rel),
    ...extra,
  });
  r.e.layout(800, 320);
  r.typed = () => r.keys.filter((k) => k.key.length === 1 && !k.ctrl && !k.alt).map((k) => k.key).join("");
  r.find = (f) => {
    const i = r.e.placed.findIndex(f);
    if (i < 0) throw new Error("no such key");
    return i;
  };
  r.idx = (l) => r.find((p) => p.def.main === l || (p.arrow ?? p.def.id) === l || (p.def.kind === "ai" && l === "ai"));
  r.down = (l, id = pid++) => {
    const p = r.e.placed[r.idx(l)];
    r.e.down(id, p.x + p.w / 2, p.y + p.h / 2);
    return id;
  };
  r.up = (id) => r.e.up(id);
  r.tap = (l) => r.up(r.down(l));
  return r;
}
const last = (r: Rig): KeyEvent => r.keys[r.keys.length - 1];

test("L4 C1 letters fire on touch down, lower case", () => {
  const r = rig();
  const id = r.down("A");
  eq(r.typed(), "a", "fired before release");
  r.up(id);
  r.tap("H");
  r.tap("I");
  eq(r.typed(), "ahi");
  eq(last(r).code, "KeyI");
  eq(r.keys[0].shift, false);
});

test("L4 C3 tap shift arms it for one key, then A types A", () => {
  const r = rig();
  r.tap("shift");
  ok(r.e.sticky.has("shift"), "armed");
  r.tap("A");
  eq(r.typed(), "A");
  eq(last(r).shift, true);
  ok(!r.e.sticky.has("shift"), "consumed (C6)");
  r.tap("B");
  eq(r.typed(), "Ab");
});

test("C3 tapping an armed modifier again disarms it", () => {
  const r = rig();
  r.tap("ctrl");
  ok(r.e.sticky.has("ctrl"));
  r.tap("ctrl");
  ok(!r.e.sticky.has("ctrl"));
  r.tap("C");
  eq(last(r).ctrl, false);
});

test("C2 C4 held shift chord with a letter; release does not arm", () => {
  const r = rig();
  const s = r.down("shift");
  ok(r.e.held.has("shift"));
  r.tap("Q");
  r.tap("W");
  r.up(s);
  eq(r.typed(), "QW");
  ok(!r.e.sticky.has("shift"), "used held mod does not arm");
  r.tap("E");
  eq(r.typed(), "QWe");
});

test("C5 same modifier on two fingers stays held until both lift", () => {
  const r = rig();
  const left = r.down("shift");
  const right = r.e.placed.findIndex((p, i) => p.def.id === "shift" && i !== r.idx("shift"));
  const p = r.e.placed[right];
  r.e.down(77, p.x + 2, p.y + 2);
  r.up(left);
  ok(r.e.held.has("shift"), "still held by the right finger");
  r.e.up(77);
  ok(!r.e.held.has("shift"));
});

test("C7 caps toggles on down; letters = shift XOR caps; LED state", () => {
  const r = rig();
  const c = r.down("caps");
  ok(r.e.caps, "on at down");
  r.up(c);
  r.tap("A");
  r.tap("shift");
  r.tap("B");
  eq(r.typed(), "Ab");
  r.tap("caps");
  ok(!r.e.caps);
});

test("C8 shift gives the upper glyph of symbol keys", () => {
  const r = rig();
  r.tap("1");
  r.tap("shift");
  r.tap("1");
  r.tap("shift");
  r.tap("[");
  r.tap("'");
  eq(r.typed(), "1!{'");
  eq(r.keys[1].code, "Digit1");
  eq(r.keys[2].code, "BracketLeft");
});

test("C9 ctrl then c is a chord, never text", () => {
  const r = rig();
  r.tap("ctrl");
  r.tap("C");
  eq(last(r).key, "c");
  eq(last(r).ctrl, true);
  eq(last(r).code, "KeyC");
  eq(r.typed(), "");
  r.tap("alt");
  r.tap("1");
  eq([last(r).key, last(r).alt, last(r).ctrl], ["1", true, false]);
});

test("C9 ctrl+shift+z keeps shift (upper key, DOM style)", () => {
  const r = rig();
  const c = r.down("ctrl");
  const s = r.down("shift");
  r.tap("Z");
  r.up(s);
  r.up(c);
  eq([last(r).key, last(r).ctrl, last(r).shift], ["Z", true, true]);
  ok(r.e.sticky.size === 0, "held mods used, none armed");
});

test("C10 space types on release; C11 a drag is the trackpad", () => {
  const r = rig();
  const sp = r.e.placed[r.idx("space")];
  r.e.down(5, sp.x + 20, sp.y + 5);
  eq(r.keys.length, 0, "space waits for release");
  r.e.up(5);
  eq(r.typed(), " ");
  eq(last(r).code, "Space");
  r.e.down(6, sp.x + 20, sp.y + 5);
  r.e.move(6, sp.x + 26, sp.y + 5);
  ok(!r.e.pad, "6px is still a tap");
  r.e.move(6, sp.x + 31, sp.y + 5);
  ok(r.e.pad, "11px starts the pad");
  ok(!r.e.pressedIndexes().has(r.idx("space")), "press visual cleared");
  r.e.move(6, sp.x + 41, sp.y + 5);
  near(r.pads[0][0], 10 * (1 + 10 * 0.06), 1e-9, "accelerated");
  near(r.pads[0][1], 0);
  r.e.up(6);
  ok(!r.e.pad);
  eq(r.typed(), " ", "a drag types no space");
});

test("C14 C15 backspace repeats after 350ms every 55ms and stops on release", () => {
  const r = rig();
  const b = r.down("backspace");
  eq(r.keys.length, 1);
  r.clock.tick(349);
  eq(r.keys.length, 1, "no repeat before 350ms");
  r.clock.tick(1 + 55);
  eq(r.keys.length, 2);
  r.clock.tick(55 * 3);
  eq(r.keys.length, 5);
  eq(last(r).key, "Backspace");
  eq(last(r).repeat, true);
  eq(r.keys[0].repeat, false);
  const ticks = r.ticks.length;
  r.up(b);
  r.clock.tick(1000);
  eq(r.keys.length, 5, "stopped");
  eq(r.ticks.length, ticks + 1, "release tick");
  eq(r.clock.pending(), 0);
});

test("C14 arrows and the stack halves repeat; letters, space and enter do not", () => {
  for (const k of ["left", "right", "up", "down"]) {
    const r = rig();
    r.down(k);
    r.clock.tick(350 + 55);
    eq(r.keys.length, 2, k);
  }
  for (const k of ["A", "enter", "tab"]) {
    const r = rig();
    r.down(k);
    r.clock.tick(2000);
    eq(r.keys.length, 1, k);
  }
  const r = rig();
  const sp = r.e.placed[r.idx("space")];
  r.e.down(9, sp.x + 4, sp.y + 4);
  r.clock.tick(2000);
  eq(r.keys.length, 0, "space never repeats");
});

test("C14 delete repeats (full layout fn row)", () => {
  const r = rig();
  r.down("delete");
  r.clock.tick(350 + 110);
  eq(r.keys.length, 3);
  eq(last(r).key, "Delete");
});

test("C16 cancel clears press, held mod and repeat", () => {
  const r = rig();
  const s = r.down("shift");
  const b = r.down("backspace");
  r.e.cancel(s);
  r.e.cancel(b);
  ok(!r.e.held.has("shift"));
  ok(r.e.pressedIndexes().size === 0);
  r.clock.tick(1000);
  eq(r.keys.length, 1);
  ok(!r.e.sticky.has("shift"), "cancel never arms");
});

test("C17 AI key calls onAI and keeps sticky mods", () => {
  const r = rig();
  r.tap("ctrl");
  r.tap("ai");
  eq(r.ai, 1);
  eq(r.keys.length, 0);
  ok(r.e.sticky.has("ctrl"));
});

test("C18 fn+F5 cycles the backlight 0>1>2>0; bare F5 is F5", () => {
  const r = rig();
  eq(r.e.backlight, 0, "E1 off by default");
  for (const want of [1, 2, 0]) {
    r.tap("fn");
    r.tap("f5");
    eq(r.e.backlight, want);
  }
  eq(r.keys.length, 0);
  r.tap("f5");
  eq(last(r).key, "F5");
});

test("C19 F9 toggles the key sound", () => {
  const r = rig();
  r.tap("f9");
  eq(r.sound, 1);
  eq(r.keys.length, 0);
  r.tap("fn");
  r.tap("f9");
  eq(last(r).key, "F9");
});

test("C20 media keys without fn; with fn they are F-keys (KB-05)", () => {
  const r = rig();
  for (const k of ["f1", "f2", "f3", "f4", "f6", "f7"]) r.tap(k);
  eq(r.media, ["mute", "volumeDown", "volumeUp", "playPause", "brightnessDown", "brightnessUp"]);
  eq(r.keys.length, 0);
  r.tap("fn");
  r.tap("f3");
  eq(last(r).key, "F3");
  eq(r.media.length, 6);
});

test("C21 C22 C23 fn layer: arrows page, home/end are F11/F12", () => {
  const r = rig();
  r.tap("fn");
  r.tap("up");
  eq(last(r).key, "PageUp");
  r.tap("fn");
  r.tap("down");
  eq(last(r).key, "PageDown");
  r.tap("fn");
  r.tap("home");
  eq(last(r).key, "F11");
  r.tap("fn");
  r.tap("end");
  eq(last(r).key, "F12");
  for (const [k, want] of [["home", "Home"], ["end", "End"], ["insert", "Insert"], ["delete", "Delete"], ["esc", "Escape"], ["f8", "F8"], ["f10", "F10"], ["up", "ArrowUp"]]) {
    r.tap(k);
    eq(last(r).key, want, k);
  }
});

test("C24 fn + number row gives F1..F12 (compact too)", () => {
  const r = rig(true);
  const keys = ["1", "2", "3", "4", "5", "6", "7", "8", "9", "0", "-", "="];
  for (const k of keys) {
    r.tap("fn");
    r.tap(k);
  }
  eq(r.keys.map((k) => k.key), keys.map((_, i) => `F${i + 1}`));
  eq(r.keys[0].code, "F1");
});

test("C25 fn + ` is Escape, fn + left/right is Home/End", () => {
  const r = rig(true);
  r.tap("fn");
  r.tap("`");
  r.tap("fn");
  r.tap("left");
  r.tap("fn");
  r.tap("right");
  eq(r.keys.map((k) => k.key), ["Escape", "Home", "End"]);
});

test("C26 C27 named keys carry live mods; tab is never text", () => {
  const r = rig();
  r.tap("tab");
  eq([last(r).key, last(r).code], ["Tab", "Tab"]);
  eq(r.typed(), "");
  r.tap("shift");
  r.tap("tab");
  eq(last(r).shift, true);
  r.tap("ctrl");
  r.tap("enter");
  eq([last(r).key, last(r).ctrl], ["Enter", true]);
});

test("A14 blank key: no fire, no tick", () => {
  const r = rig();
  r.tap("blank");
  eq(r.keys.length + r.ticks.length, 0);
});

test("D1 D2 ticks: press on every key, release for normal, mod, repeat and space", () => {
  const r = rig();
  r.tap("A");
  eq(r.ticks, [false, true]);
  r.tap("shift");
  eq(r.ticks.slice(2), [false, true]);
  const sp = r.e.placed[r.idx("space")];
  r.e.down(3, sp.x + 3, sp.y + 3);
  r.e.up(3);
  eq(r.ticks.slice(4), [false, true]);
  r.tap("caps");
  eq(r.ticks.slice(6), [false, true], "caps: press and release");
  const b = r.down("backspace");
  r.clock.tick(350 + 55);
  r.up(b);
  eq(r.ticks.slice(8), [false, false, true], "repeat ticks then release");
});

test("relayout on size and compact change", () => {
  const r = rig();
  const n = r.e.placed.length;
  r.e.layout(800, 320, true);
  ok(r.e.placed.length < n);
  r.e.layout(400, 200, true);
  near(r.e.placed[0].x, 8);
  eq(r.e.compact, true);
});

test("onChange fires when state changes", () => {
  let n = 0;
  const r = rig(false, { onChange: () => n++ });
  r.tap("A");
  ok(n >= 2, "press and release redraw");
});
