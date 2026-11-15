import { RadialMenu, type Category, type MenuOptions } from "../src/menu";
import { mountMenu } from "../src/index";
import { FakeClock } from "./fakeclock";
import { FakeDocument, FakeElement, FakeWindow, attr, pointer } from "./fakedom";
import { eq, near, ok, test } from "./t";

// The box is 636 px at (0, 0): the design space is the page shifted by
// the 18 px padding, scale 1.
const P = (x: number, y: number): [number, number] => [x + 18, y + 18];
const TOP = P(300, 130); // the first action
const RIGHT = P(470, 300); // the second of four
const HUB_P = P(300, 300);
const cat = (i: number, n: number): [number, number] => {
  const a = ((180 + (i * 360) / n - 90) * Math.PI) / 180;
  return P(300 + 86 * Math.cos(a), 300 + 86 * Math.sin(a));
};

interface Rig {
  m: RadialMenu;
  stage: FakeElement;
  win: FakeWindow;
  clock: FakeClock;
  ran: string[];
  favs: string[][];
  closed: number;
}

function cats(ran: string[]): Category[] {
  const it = (id: string, icon = "check") => ({ id, label: id, icon, detail: id + " detail", run: () => ran.push(id) });
  return [
    { id: "edit", label: "EDIT", icon: "open", items: [it("open", "open"), it("find", "find"), it("format", "format"), it("rename")] },
    { id: "git", label: "GIT", icon: "branch", items: [it("commit"), it("push", "up"), it("pull", "down")] },
    { id: "run", label: "RUN", icon: "shell", items: [it("build", "shell"), it("test")] },
  ];
}

function rig(o: Partial<MenuOptions> = {}): Rig {
  const win = new FakeWindow();
  win.innerWidth = 1000;
  win.innerHeight = 1000;
  const clock = new FakeClock(win);
  const stage = new FakeElement("div");
  const r = { win, clock, stage, ran: [] as string[], favs: [] as string[][], closed: 0 } as Rig;
  r.m = new RadialMenu(stage as unknown as HTMLElement, {
    categories: cats(r.ran),
    startOpen: true,
    onClosed: () => r.closed++,
    onFavoritesChange: (ids) => r.favs.push(ids),
    clock,
    doc: new FakeDocument() as unknown as Document,
    win: win as unknown as Window,
    ...o,
  });
  r.m.box = { left: 0, top: 0, side: 636 };
  clock.tick(600); // entrance + bloom
  return r;
}

function tap(r: Rig, p: [number, number], hold = 40): void {
  const id = 7;
  r.stage.dispatchEvent(pointer("pointerdown", p[0], p[1], id));
  r.clock.tick(hold);
  r.stage.dispatchEvent(pointer("pointerup", p[0], p[1], id));
}

const by = (r: Rig, c: string): FakeElement[] => r.stage.byClass(c);

test("X1 F1 the rings: SAVED first, then the categories; the first category sits at the bottom", () => {
  const r = rig({ favorites: ["find"] });
  const c = by(r, "rm-cat");
  eq(c.map((e) => attr(e, "data-id")), ["saved", "edit", "git", "run"]);
  eq(attr(c[0], "aria-checked"), "true", "SAVED is active");
  eq(by(r, "rm-act").length, 1, "one favourite");
  eq(r.m.activeIndex, 0);
  const names = by(r, "rm-cname").map((e) => e.textContent);
  eq(names, ["SAVED"]);
  eq(by(r, "rm-csub")[0].textContent, "tap to close");
});

test("C2 C3 C7 the design's gradients and colours", () => {
  const r = rig();
  const stops = r.stage.all((e) => e.tagName === "RADIALGRADIENT");
  const g = (id: string) => stops.find((s) => attr(s, "id").endsWith("-" + id))!;
  ok(g("cat") && attr(g("cat"), "r") === "116");
  eq(g("cat").children.map((s) => attr(s, "style")), ["stop-color:#24242F", "stop-color:#131319"]);
  eq(g("wedge").children.map((s) => attr(s, "offset")), ["0.5", "1"]);
  eq(attr(g("wedgeAcc"), "r"), "212");
  ok(attr(g("catActive").children[0], "style").includes("#264F78"));
  const active = by(r, "rm-cat")[0];
  ok(attr(active, "fill").includes("catActive"));
  // icons: active category #AEDAFF, others #BEBEC8
  const icons = r.stage.all((e) => e.attrs.has("data-icon") && e.parentNode?.classList.contains("rm-inner") === true);
  ok(icons[0].innerHTML.includes("#AEDAFF"), icons[0].innerHTML);
  ok(icons[1].innerHTML.includes("#BEBEC8"));
});

test("S3 M5 a category tap switches the actions (no back) and replays the bloom", () => {
  const r = rig();
  tap(r, cat(1, 4));
  eq(r.m.activeIndex, 1);
  eq(by(r, "rm-act").length, 4);
  eq(by(r, "rm-cname")[0].textContent, "EDIT");
  // the bloom replays: the last petal waits its turn
  r.clock.tick(16);
  eq(attr(by(r, "rm-item")[3], "visibility"), "hidden");
  r.clock.tick(600);
  eq(attr(by(r, "rm-item")[3], "visibility"), "visible");
  tap(r, cat(1, 4));
  eq(r.m.activeIndex, 1, "the same category: nothing");
});

test("S4 X3 tapping an action flashes, closes and runs it", () => {
  const r = rig();
  tap(r, cat(1, 4));
  r.clock.tick(600);
  tap(r, TOP);
  eq(r.ran, ["open"]);
  ok(r.m.closing, "the suck-in starts");
  eq(r.closed, 0);
  r.clock.tick(320);
  eq(r.closed, 1, "onClosed after the 300 ms close");
  tap(r, RIGHT);
  eq(r.ran, ["open"], "a closing menu takes no more taps");
});

test("S4 X7 an action without run shows the design's toast for 1.5 s", () => {
  const r = rig({ categories: [{ id: "x", label: "X", icon: "star", items: [{ id: "a", label: "ask ai", icon: "ask" }] }], saved: false });
  tap(r, TOP);
  eq(r.m.toastText, "▸ run ask ai");
  ok(!r.m.closing);
  r.clock.tick(1400);
  eq(r.m.toastText, "▸ run ask ai");
  r.clock.tick(200);
  eq(r.m.toastText, null);
});

test("S5 a press released on another wedge does nothing", () => {
  const r = rig();
  tap(r, cat(1, 4));
  r.clock.tick(600);
  r.stage.dispatchEvent(pointer("pointerdown", TOP[0], TOP[1], 3));
  r.stage.dispatchEvent(pointer("pointerup", RIGHT[0], RIGHT[1], 3));
  eq(r.ran, []);
});

test("S6 M8 G15 a 520 ms hold fills the arc, swells the petal and opens the card; the release does not run", () => {
  const r = rig();
  tap(r, cat(1, 4));
  r.clock.tick(600);
  r.stage.dispatchEvent(pointer("pointerdown", TOP[0], TOP[1], 4));
  r.clock.tick(48);
  const prog = by(r, "rm-prog")[0];
  eq(attr(prog, "opacity"), "0", "the arc waits for 16 %");
  r.clock.tick(160);
  eq(attr(prog, "opacity"), "1");
  const len = Number(attr(prog, "stroke-dasharray"));
  const off = Number(attr(prog, "stroke-dashoffset"));
  ok(off > 0 && off < len * 0.7, "filling: " + off + "/" + len);
  ok(attr(by(r, "rm-item")[0], "transform").includes("scale(1.0"), "the held petal swells");
  ok(by(r, "rm-item")[0].classList.contains("rm-hot"));
  ok(attr(by(r, "rm-act")[0], "fill").includes("wedgeHot"));
  eq(r.m.tipShown, null);
  r.clock.tick(330);
  eq(r.m.tipShown?.id, "open", "the card opens at 520 ms");
  const tip = by(r, "rm-tip")[0];
  ok(!tip.hidden && tip.classList.contains("rm-show"));
  eq(by(r, "rm-tip-title")[0].textContent, "open");
  eq(by(r, "rm-tip-desc")[0].textContent, "open detail");
  eq(by(r, "rm-tip-fav")[0].textContent, "☆  add to favorites");
  eq(attr(prog, "opacity"), "0", "the arc resets");
  r.stage.dispatchEvent(pointer("pointerup", TOP[0], TOP[1], 4));
  eq(r.ran, [], "the release after the hold does not run");
  tap(r, TOP);
  eq(r.ran, ["open"], "a later tap runs");
});

test("S7 sliding off the wedge or a cancel resets the hold", () => {
  const r = rig();
  tap(r, cat(1, 4));
  r.clock.tick(600);
  r.stage.dispatchEvent(pointer("pointerdown", TOP[0], TOP[1], 5));
  r.clock.tick(200);
  r.stage.dispatchEvent(pointer("pointermove", RIGHT[0], RIGHT[1], 5));
  r.clock.tick(500);
  eq(r.m.tipShown, null);
  r.stage.dispatchEvent(pointer("pointerdown", TOP[0], TOP[1], 6));
  r.clock.tick(200);
  r.stage.dispatchEvent(pointer("pointercancel", TOP[0], TOP[1], 6));
  r.clock.tick(500);
  eq(r.m.tipShown, null);
  eq(r.ran, []);
});

test("S8 S11 D9 an empty press hides the card, but not the release right after it opened", () => {
  const r = rig();
  tap(r, cat(1, 4));
  r.clock.tick(600);
  r.stage.dispatchEvent(pointer("pointerdown", TOP[0], TOP[1], 8));
  r.clock.tick(540);
  ok(r.m.tipShown);
  const empty = P(300, 300 - 122); // between the rings
  r.stage.dispatchEvent(pointer("pointerdown", empty[0], empty[1], 9));
  ok(r.m.tipShown, "inside the 450 ms guard");
  r.clock.tick(500);
  r.stage.dispatchEvent(pointer("pointerdown", empty[0], empty[1], 10));
  eq(r.m.tipShown, null);
});

test("F2 F3 M10 ★ favorite: saved with a toast and the pulse; the open SAVED ring follows; removed again", () => {
  const r = rig({ favorites: [] });
  ok(r.m.toastText === null);
  tap(r, cat(2, 4)); // GIT
  r.clock.tick(600);
  r.stage.dispatchEvent(pointer("pointerdown", RIGHT[0], RIGHT[1], 11)); // push (2nd of 3 is at 120°, RIGHT is 90°: inside)
  r.clock.tick(540);
  eq(r.m.tipShown?.id, "push");
  by(r, "rm-tip-fav")[0].dispatchEvent({ type: "click" });
  eq(r.favs, [["push"]]);
  eq(r.m.toastText, "★ saved push");
  ok(r.m.pulsing);
  eq(by(r, "rm-tip-fav")[0].textContent, "★  favorited");
  ok(by(r, "rm-tip-fav")[0].classList.contains("rm-on"));
  by(r, "rm-tip-fav")[0].dispatchEvent({ type: "click" });
  eq(r.favs[1], []);
  eq(r.m.toastText, "removed push from saved");
  by(r, "rm-tip-fav")[0].dispatchEvent({ type: "click" });
  r.stage.dispatchEvent(pointer("pointerup", RIGHT[0], RIGHT[1], 11));
  tap(r, cat(0, 4)); // SAVED
  eq(by(r, "rm-act").map((e) => attr(e, "aria-label")), ["push: push detail"]);
});

test("F1 SAVED keeps the favourites' order, holds 8 at most, and hints when empty", () => {
  const r = rig({ favorites: ["test", "open", "nope"] });
  eq(by(r, "rm-item").map((e) => attr(e, "data-id")), ["test", "open"], "unknown ids are left out");
  const many = rig({ favorites: ["open", "find", "format", "rename", "commit", "push", "pull", "build"] });
  many.m.toggleFav({ id: "test", label: "test", icon: "check" });
  eq(many.m.toastText, "saved is full: remove one first");
  eq(many.m.favorites.length, 8);
  const empty = rig({ favorites: ["open"] });
  tap(empty, cat(1, 4));
  tap(empty, cat(0, 4));
  eq(empty.m.toastText, null, "a SAVED ring with items needs no hint");
  empty.m.setFavorites([]);
  tap(empty, cat(1, 4));
  tap(empty, cat(0, 4));
  eq(empty.m.toastText, "hold any action to save it here");
});

test("S2 M7 C15 standalone: closed hub shows ✦ menu, the hub opens and folds the rings", () => {
  const r = rig({ startOpen: false, onClosed: undefined });
  eq(by(r, "rm-csub")[0].textContent, "menu");
  ok(by(r, "rm-hub")[0].classList.contains("rm-closed"));
  eq(attr(by(r, "rm-rings")[0], "visibility"), "hidden");
  tap(r, P(300, 300 - 170));
  eq(r.ran, [], "a closed menu has only its hub");
  tap(r, HUB_P);
  ok(r.m.open);
  r.clock.tick(100);
  const mid = by(r, "rm-rings")[0];
  ok(Number(attr(mid, "opacity")) > 0 && Number(attr(mid, "opacity")) < 1, "the rings fade in");
  ok(attr(mid, "transform").includes("rotate(-"), "with the settle twist");
  r.clock.tick(400);
  eq(attr(mid, "opacity"), "1");
  eq(by(r, "rm-cname")[0].textContent, "SAVED");
  tap(r, HUB_P);
  ok(!r.m.open);
  r.clock.tick(400);
  eq(attr(mid, "visibility"), "hidden", "folded back in place");
});

test("M1 M19 the entrance; reduced motion skips every animation", () => {
  const win = new FakeWindow();
  const clock = new FakeClock(win);
  const stage = new FakeElement("div");
  new RadialMenu(stage as unknown as HTMLElement, { categories: cats([]), startOpen: true, origin: [0.9, 0.8], clock, doc: new FakeDocument() as unknown as Document, win: win as unknown as Window });
  const en = stage.byClass("rm-entrance")[0];
  eq(en.style.getPropertyValue("opacity"), "0");
  eq(en.style.getPropertyValue("transform"), "scale(0.9)");
  eq(en.style.getPropertyValue("transform-origin"), "90% 80%", "grows from the button");
  clock.tick(420);
  eq(en.style.getPropertyValue("opacity"), "1");
  const calm = new FakeElement("div");
  const m = new RadialMenu(calm as unknown as HTMLElement, { categories: cats([]), favorites: ["open", "find", "format"], startOpen: true, reducedMotion: true, clock, doc: new FakeDocument() as unknown as Document, win: win as unknown as Window });
  eq(calm.byClass("rm-entrance")[0].style.getPropertyValue("opacity"), "1");
  eq(calm.byClass("rm-item").map((e) => attr(e, "visibility")), ["visible", "visible", "visible"], "no bloom");
  m.toggleFav({ id: "build", label: "build", icon: "shell" });
  ok(!m.pulsing, "no pulse");
});

test("M4 C18 the bloom: petals open clockwise, the spark rides the front", () => {
  const win = new FakeWindow();
  const clock = new FakeClock(win);
  const stage = new FakeElement("div");
  new RadialMenu(stage as unknown as HTMLElement, { categories: cats([]), favorites: ["open", "find", "format", "rename", "commit", "push"], startOpen: true, clock, doc: new FakeDocument() as unknown as Document, win: win as unknown as Window });
  clock.tick(32);
  const vis = () => stage.byClass("rm-item").map((e) => attr(e, "visibility") === "visible");
  const early = vis();
  ok(early[0] && !early[5], "first in, last waiting: " + early);
  eq(attr(stage.byClass("rm-spark")[0], "visibility"), "visible");
  clock.tick(600);
  eq(vis(), [true, true, true, true, true, true]);
  eq(attr(stage.byClass("rm-spark")[0], "visibility"), "hidden");
});

test("M6 the close from the button: fade, swirl and shrink toward the button", () => {
  const r = rig({ origin: [0.9, 0.9] });
  r.m.requestClose();
  r.clock.tick(150);
  const c = by(r, "rm-collapse")[0];
  const op = Number(c.style.getPropertyValue("opacity"));
  ok(op > 0 && op < 1, "fading: " + op);
  ok(c.style.getPropertyValue("transform").includes("rad) scale(0."), c.style.getPropertyValue("transform"));
  eq(c.style.getPropertyValue("transform-origin"), "90% 90%");
  r.clock.tick(200);
  eq(r.closed, 1);
});

test("P5 a long press on the menu never opens the page's context menu; the padding belongs to the page", () => {
  const r = rig();
  const e = { type: "contextmenu" } as Parameters<FakeElement["dispatchEvent"]>[0];
  r.stage.dispatchEvent(e);
  ok(e.defaultPrevented);
  const out = pointer("pointerdown", 5, 5, 20);
  let stopped = false;
  out.stopPropagation = () => (stopped = true);
  r.stage.dispatchEvent(out);
  ok(!stopped, "a press in the padding goes on to the tap catcher");
});

test("keyboard: Enter runs the focused action, arrows move, Escape closes", () => {
  const r = rig();
  tap(r, cat(1, 4));
  r.clock.tick(600);
  const acts = by(r, "rm-act");
  eq(attr(acts[0], "role"), "menuitem");
  eq(attr(acts[0], "tabindex"), "0");
  acts[0].dispatchEvent({ type: "keydown", key: "ArrowRight" });
  ok(acts[1].focused, "the next action has the focus");
  acts[1].dispatchEvent({ type: "keydown", key: "ContextMenu" });
  eq(r.m.tipShown?.id, "find", "the context-menu key opens the details");
  acts[1].dispatchEvent({ type: "keydown", key: "Escape" });
  eq(r.m.tipShown, null, "Escape closes the card first");
  acts[1].dispatchEvent({ type: "keydown", key: "Enter" });
  eq(r.ran, ["find"]);
  const k = rig();
  by(k, "rm-svg")[0].dispatchEvent({ type: "keydown", key: "Escape" });
  ok(k.m.closing);
});

test("P5 mountMenu draws the design's page inline: closed hub first", () => {
  const win = new FakeWindow();
  const host = new FakeElement("div");
  const m = mountMenu(host as unknown as HTMLElement, { categories: cats([]), clock: new FakeClock(win), doc: new FakeDocument() as unknown as Document, win: win as unknown as Window });
  ok(host.byClass("rm-inline").length === 1);
  ok(!m.open);
  near(m.box.side, 0);
});
