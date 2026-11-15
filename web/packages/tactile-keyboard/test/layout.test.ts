import { DELL_LAYOUT, autoCompact, defaultHeight, hitTest, placeKeys } from "../src/layout";
import { eq, near, ok, test } from "./t";

const flex = (r: number): number[] => DELL_LAYOUT[r].map((k) => k.flex);

test("A1 six rows in Dell US-ANSI order", () => {
  eq(DELL_LAYOUT.length, 6);
  eq(DELL_LAYOUT[1].map((k) => k.main ?? k.id).join(" "), "` 1 2 3 4 5 6 7 8 9 0 - = backspace");
  eq(DELL_LAYOUT[2].map((k) => k.main ?? k.id).join(" "), "tab Q W E R T Y U I O P [ ] \\");
  eq(DELL_LAYOUT[3].map((k) => k.main ?? k.id).join(" "), "caps A S D F G H J K L ; ' enter");
  eq(DELL_LAYOUT[4].map((k) => k.main ?? k.id).join(" "), "shift Z X C V B N M , . / shift");
  eq(DELL_LAYOUT[5].map((k) => k.id ?? k.kind).join(" "), "ctrl fn ai alt space alt ctrl left arrowStack right");
});

test("A2 function row ids, labels and F-number tags", () => {
  const r = DELL_LAYOUT[0];
  eq(r.map((k) => k.id).join(" "), "esc f1 f2 f3 f4 f5 f6 f7 f8 f9 f10 home end insert delete blank");
  eq(r.map((k) => k.fnum ?? "").join(","), ",F1,F2,F3,F4,F5,F6,F7,F8,F9,F10,F11,F12,,,");
  eq(r.map((k) => k.label ?? "").join(","), "esc,,,,,,,,,,prt sc,home,end,insert,delete,");
  eq(r.map((k) => k.icon ?? "").join(","), ",volume_off,volume_mute,volume_up,play_arrow,keyboard,brightness_low,brightness_high,desktop_windows,,,,,,,");
  ok(flex(0).every((f) => f === 1), "fn row flex 1");
});

test("A3-A7 flex widths per key", () => {
  eq(flex(1), [1.2, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1.8]);
  eq(flex(2), [1.8, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1.2]);
  eq(flex(3), [2, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2]);
  eq(flex(4), [2.5, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2.5]);
  eq(flex(5), [1.5, 1, 1, 1, 5.5, 1, 1, 1, 1, 1]);
  eq(DELL_LAYOUT[1].map((k) => k.shifted ?? "").join(""), "~!@#$%^&*()_+");
  eq(DELL_LAYOUT[3][0].led, true, "caps LED");
  eq(DELL_LAYOUT[5][6].secondaryIcon, "menu", "right ctrl menu mark");
});

test("A8 A9 A11 full layout math (800x320)", () => {
  const p = placeKeys(800, 320, false);
  const unit = (320 - 16 - 3 * 5) / 5.7;
  near(p[0].x, 8);
  near(p[0].y, 8);
  near(p[0].h, 0.7 * unit, 1e-9, "fn row is 0.7");
  const innerW0 = 800 - 16 - 1.5 * 15;
  near(p[0].w, innerW0 / 16, 1e-9, "fn row key width, key gap 1.5");
  near(p[1].x, 8 + innerW0 / 16 + 1.5);
  const num = p.filter((k) => k.row === 1);
  const innerW1 = 800 - 16 - 3 * 13;
  near(num[0].w, (innerW1 * 1.2) / 15, 1e-9, "backquote 1.2 of 15");
  near(num[0].y, 8 + 0.7 * unit + 3, 1e-9, "row gap 3");
  near(num[13].w, (innerW1 * 1.8) / 15, 1e-9, "backspace 1.8");
  near(num[13].x + num[13].w, 800 - 8, 1e-9, "row ends at the pad");
});

test("A10 compact drops the fn row, 5 equal rows, gaps 1.5", () => {
  const p = placeKeys(390, 380, true);
  ok(!p.some((k) => k.def.id === "esc" || k.def.id === "f1"), "no fn row");
  eq(p[0].def.main, "`");
  const unit = (380 - 16 - 1.5 * 4) / 5;
  near(p[0].h, unit);
  const tab = p.find((k) => k.def.id === "tab")!;
  near(tab.y, 8 + unit + 1.5);
  const innerW = 390 - 16 - 1.5 * 13;
  near(p[0].w, (innerW * 1.2) / 15);
});

test("A12 arrow stack splits into flush up/down halves", () => {
  const p = placeKeys(800, 320, false);
  const up = p.find((k) => k.arrow === "up")!;
  const down = p.find((k) => k.arrow === "down")!;
  eq(up.id, "up");
  eq(down.id, "down");
  near(up.h * 2, p.find((k) => k.def.id === "left")!.h);
  near(down.y, up.y + up.h, 1e-9, "flush");
  near(up.x, down.x);
});

test("A13 hit test finds keys and misses gaps", () => {
  const p = placeKeys(800, 320, false);
  const a = p.findIndex((k) => k.def.main === "A");
  eq(hitTest(p, p[a].x + 2, p[a].y + 2), a);
  eq(hitTest(p, p[a].x - 1, p[a].y + 2), -1, "the 3px gap left of A");
  eq(hitTest(p, 2, 2), -1, "the pad");
});

test("A4 posX runs -1 (left) .. +1 (right)", () => {
  const p = placeKeys(800, 320, false);
  const l = p.find((k) => k.def.id === "tab")!;
  const r = p.find((k) => k.def.main === "\\")!;
  ok(l.posX < -0.8 && r.posX > 0.8, `posX ${l.posX} ${r.posX}`);
  near(l.posX, ((l.x + l.w / 2) / 800) * 2 - 1);
});

test("A15 default height is width*0.40 clamped 180..360", () => {
  eq(defaultHeight(300), 180);
  eq(defaultHeight(500), 200);
  eq(defaultHeight(1200), 360);
});

test("A17 compact when the short side is under 720", () => {
  eq(autoCompact(390, 844), true, "phone portrait");
  eq(autoCompact(844, 390), true, "phone landscape (screenshot 04)");
  eq(autoCompact(1024, 768), false, "tablet landscape");
  eq(autoCompact(768, 1024), false, "tablet portrait");
});
