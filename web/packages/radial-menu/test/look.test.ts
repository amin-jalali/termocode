// The design's look, 1:1 (mobocode-radial-v6.html, final_blue.png): the
// menu brings its own dark palette and its own dark disc, so a light page
// never shows through between the wedges; the outer ring always has six
// equal wedges; every stroke, gradient and label is the design's.
import css from "../src/radial-menu.css";
import { RadialMenu, type Category, type MenuOptions } from "../src/menu";
import * as geo from "../src/geometry";
import { RO2, stageSide, ringRadius } from "../src/geometry";
import { FakeClock } from "./fakeclock";
import { FakeDocument, FakeElement, FakeWindow, attr, pointer } from "./fakedom";
import { eq, near, ok, test } from "./t";

// The disc's radius and the outer ring's least wedge count (design units).
const { DISC_R, MIN_SLOTS } = geo as unknown as { DISC_R: number; MIN_SLOTS: number };
const hitTest = geo.hitTest as unknown as (x: number, y: number, open: boolean, cats: number, items: number, slots?: number) => geo.Hit;

/** decls are the declarations of the rule whose selector list is sel. */
function decls(sel: string): Record<string, string> {
  const flat = (css as string).replace(/\/\*[\s\S]*?\*\//g, "");
  const re = /([^{}]+)\{([^{}]*)\}/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(flat))) {
    const sels = m[1].split(",").map((s) => s.trim());
    if (sels.includes(sel)) {
      const out: Record<string, string> = {};
      for (const d of m[2].split(";")) {
        const i = d.indexOf(":");
        if (i > 0) out[d.slice(0, i).trim()] = d.slice(i + 1).trim().toLowerCase();
      }
      return out;
    }
  }
  throw new Error("no rule " + sel);
}

const it = (id: string, icon = "check") => ({ id, label: id, icon, detail: id + " detail", run: () => undefined });

function menu(cats: Category[], o: Partial<MenuOptions> = {}): { m: RadialMenu; stage: FakeElement } {
  const win = new FakeWindow();
  const clock = new FakeClock(win);
  const stage = new FakeElement("div");
  const m = new RadialMenu(stage as unknown as HTMLElement, {
    categories: cats,
    startOpen: true,
    saved: false,
    reducedMotion: true,
    clock,
    doc: new FakeDocument() as unknown as Document,
    win: win as unknown as Window,
    ...o,
  });
  m.box = { left: 0, top: 0, side: 636 };
  return { m, stage };
}

test("look: the palette is the design's own and fixed, whatever the page's theme", () => {
  const v = decls(".rm-inline");
  eq(decls(".rm-root"), v, "the floating and the inline menu share one palette");
  eq(v["--rm-accent"], "#007acc");
  eq(v["--rm-accent-dim"], "#0e639c");
  eq(v["--rm-ink2"], "#9a9aa4");
  eq(v["--rm-dim"], "#6a6a75");
  eq(v["--rm-line"], "#272730", "the design's --line");
  eq(v["color-scheme"], "dark", "dark form controls and scrollbars on a light page too");
  // Nothing reads the page's own variables (its light theme's colours).
  const foreign = (css as string).match(/var\(--(?!rm-)[a-z0-9-]+/g);
  eq(foreign, null, "the stylesheet reads a page variable");
});

test("look: one dark disc behind the rings, with the design's stage glow and drop shadow", () => {
  const { stage } = menu([{ id: "a", label: "A", icon: "ask", items: [it("x")] }]);
  const disc = stage.byClass("rm-disc");
  eq(disc.length, 1, "the dark disc");
  const d = decls(".rm-disc");
  eq(d["border-radius"], "50%");
  ok(d["background"].includes("rgba(0, 122, 204, 0.07)"), "the faint blue glow: " + d["background"]);
  ok(d["background"].includes("#15151d") && d["background"].includes("#0c0c11"), "the stage's dark gradient: " + d["background"]);
  ok(/#000\b/.test(d["box-shadow"]), "the big drop shadow: " + d["box-shadow"]);
  ok(DISC_R > RO2, "the disc reaches past the outer ring");
  near(Number(attr(stage.byClass("rm-gapring")[0], "r")), 122);
});

test("look: the design's strokes", () => {
  eq(decls(".rm-gapring")["stroke"], "#1c1c24");
  eq(decls(".rm-gapring")["stroke-width"], "1");
  eq(decls(".rm-act")["stroke"], "var(--rm-line)");
  eq(decls(".rm-cat")["stroke"], "var(--rm-line)");
  eq(decls(".rm-slot")["stroke"], "var(--rm-line)", "an empty wedge has the same hairline");
  eq(decls(".rm-item.rm-hot .rm-act")["stroke"], "#34343f");
  eq(decls(".rm-hub")["stroke"], "#22222b");
  eq(decls(".rm-hub.rm-closed")["stroke"], "var(--rm-accent-dim)");
  eq(decls(".rm-hubring")["stroke"], "rgba(255, 255, 255, 0.04)");
  const prog = decls(".rm-prog");
  eq([prog["stroke"], prog["stroke-width"], prog["stroke-linecap"]], ["var(--rm-accent)", "3.5", "round"]);
  eq(prog["filter"], "drop-shadow(0 0 4px rgba(0, 122, 204, 0.6))");
  const on = decls(".rm-cat.rm-on");
  eq([on["stroke"], on["stroke-width"], on["filter"]], ["var(--rm-accent-dim)", "1.3", "drop-shadow(0 0 6px rgba(0, 122, 204, 0.25))"]);
  eq(decls(".rm-item")["color"], "#c5c6cd");
  eq(decls(".rm-item.rm-hot")["color"], "#eaf2f5");
});

test("look: the design's gradients and icon colours", () => {
  const { stage } = menu([
    { id: "a", label: "A", icon: "ask", items: [it("x")] },
    { id: "b", label: "B", icon: "open", items: [it("y")] },
  ]);
  const grads = stage.all((e) => e.tagName === "RADIALGRADIENT");
  const g = (id: string) => {
    const x = grads.find((s) => attr(s, "id").endsWith("-" + id));
    if (!x) throw new Error("no gradient " + id);
    return [attr(x, "r"), ...x.children.map((s) => attr(s, "offset") + " " + attr(s, "stop-color").toLowerCase())];
  };
  eq(g("wedge"), ["212", "0.55 #1d1d27", "1 #141419"]);
  eq(g("wedgeHot"), ["212", "0.55 #26262f", "1 #1a1a20"]);
  eq(g("wedgeAcc"), ["212", "0.5 #16456e", "1 #0e2d49"]);
  eq(g("cat"), ["116", "0.4 #1b1b23", "1 #15151b"]);
  eq(g("catHover"), ["116", "0.4 #23232d", "1 #1b1b22"]);
  eq(g("catActive"), ["116", "0.3 #264f78", "1 #15314c"]);
  const icons = stage.all((e) => e.attrs.has("data-icon") && e.parentNode?.classList.contains("rm-inner") === true);
  const cols = (x: FakeElement) => x.all((e) => e !== x).map((e) => (attr(e, "stroke") || attr(e, "fill")).toLowerCase());
  ok(cols(icons[0]).includes("currentcolor") && icons[0].classList.contains("rm-accent"), "the active category's icon is the accent blue (final_blue.png): " + cols(icons[0]));
  ok(cols(icons[1]).includes("#9a9aa4"), "the other icons: " + cols(icons[1]));
});

test("look: the outer ring always has six equal wedges; the empty ones are drawn and take no taps", () => {
  const ran: string[] = [];
  const run = (id: string) => ({ ...it(id), run: () => ran.push(id) });
  const { m, stage } = menu([{ id: "a", label: "A", icon: "ask", items: [run("one"), run("two"), run("three")] }]);
  const outer = stage.byClass("rm-outer")[0];
  eq(attr(outer, "data-slots"), String(MIN_SLOTS));
  eq(stage.byClass("rm-act").length, 3, "three actions");
  eq(stage.byClass("rm-slot").length, 3, "and three empty wedges");
  // every wedge is 60° wide (less the 0.7° gaps): the second action sits at 60°
  const pt = (deg: number): [number, number] => {
    const a = ((deg - 90) * Math.PI) / 180;
    return [18 + 300 + 170 * Math.cos(a), 18 + 300 + 170 * Math.sin(a)];
  };
  const tap = (p: [number, number]) => {
    stage.dispatchEvent(pointer("pointerdown", p[0], p[1], 3));
    stage.dispatchEvent(pointer("pointerup", p[0], p[1], 3));
  };
  tap(pt(240)); // an empty wedge
  eq(ran, [], "an empty wedge runs nothing");
  ok(!m.closing);
  eq(hitTest(300 + 170 * Math.sin(Math.PI / 3), 300 - 170 * Math.cos(Math.PI / 3), true, 1, 3, 6), { kind: "act", i: 1 });
  eq(hitTest(300, 300 + 170, true, 1, 3, 6), null, "the 4th of 6 wedges is empty");
  // more than six: one wedge each
  const big = menu([{ id: "a", label: "A", icon: "ask", items: ["1", "2", "3", "4", "5", "6", "7", "8"].map((x) => it(x)) }]);
  eq(big.stage.byClass("rm-act").length, 8);
  eq(big.stage.byClass("rm-slot").length, 0);
  // an empty SAVED ring is six empty wedges, not a hole
  const none = menu([{ id: "a", label: "A", icon: "ask", items: [] }]);
  eq(none.stage.byClass("rm-slot").length, 6);
});

test("look: the centre labels sit on the design's baselines (no re-centring)", () => {
  const { stage } = menu([{ id: "a", label: "AI", icon: "ask", items: [it("x")] }]);
  const name = stage.byClass("rm-cname")[0], sub = stage.byClass("rm-csub")[0];
  eq([attr(name, "y"), attr(sub, "y")], ["299", "318"]);
  eq([attr(name, "dominant-baseline"), attr(sub, "dominant-baseline")], ["", ""]);
  const n = decls(".rm-cname"), s = decls(".rm-csub");
  eq([n["font-size"], n["font-weight"], n["letter-spacing"]], ["17px", "800", ".16em"]);
  eq([s["font-size"], s["font-weight"], s["letter-spacing"]], ["8.5px", "600", ".22em"]);
});

test("look: a phone shows the menu at the design's own size (444 px rings box), the disc on screen", () => {
  eq(stageSide(390, 664), 480, "an iPhone in Safari: the design's 480 box");
  eq(stageSide(844, 390), 480, "a landscape phone");
  const s = stageSide(320, 568);
  ok(s < 480, "a small phone shrinks it");
  near((2 * DISC_R * (s - 36)) / 600, 320 - 24, 1e-6, "the whole disc fits, 12 px from each edge");
  near(ringRadius(480), (DISC_R / 600) * 444);
});
