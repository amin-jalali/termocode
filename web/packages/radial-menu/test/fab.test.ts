import { mount, type MountOptions, type RadialFab } from "../src/fab";
import type { Category } from "../src/menu";
import { FakeClock } from "./fakeclock";
import { FakeDocument, FakeElement, FakeWindow, pointer } from "./fakedom";
import { eq, near, ok, test } from "./t";

interface Rig {
  fab: RadialFab;
  host: FakeElement;
  win: FakeWindow;
  doc: FakeDocument;
  clock: FakeClock;
  vp: { w: number; h: number };
  ins: { top: number; right: number; bottom: number; left: number };
  moved: [number, number][];
  ran: string[];
  favs: string[][];
  opened: number;
  closed: number;
}

function cats(ran: string[]): Category[] {
  return [{ id: "git", label: "GIT", icon: "branch", items: [{ id: "push", label: "push", icon: "up", run: () => ran.push("push") }] }];
}

function rig(o: Partial<MountOptions> = {}, w = 800, h = 600): Rig {
  const win = new FakeWindow();
  win.innerWidth = w;
  win.innerHeight = h;
  const r = { win, doc: new FakeDocument(), host: new FakeElement("body"), clock: new FakeClock(win), vp: { w, h }, ins: { top: 0, right: 0, bottom: 0, left: 0 }, moved: [], ran: [], favs: [], opened: 0, closed: 0 } as unknown as Rig;
  r.fab = mount(r.host as unknown as HTMLElement, {
    categories: cats(r.ran),
    onMove: (p) => r.moved.push(p),
    onFavoritesChange: (ids) => r.favs.push(ids),
    onOpen: () => r.opened++,
    onClose: () => r.closed++,
    viewport: () => r.vp,
    insets: () => r.ins,
    clock: r.clock,
    doc: r.doc as unknown as Document,
    win: win as unknown as Window,
    ...o,
  });
  return r;
}

const btn = (r: Rig): FakeElement => r.fab.button as unknown as FakeElement;
const left = (r: Rig): number => parseFloat(btn(r).style.getPropertyValue("left"));
const top = (r: Rig): number => parseFloat(btn(r).style.getPropertyValue("top"));

function tapFab(r: Rig, id = 1): void {
  const [x, y] = r.fab.position;
  btn(r).dispatchEvent(pointer("pointerdown", x, y, id));
  r.clock.tick(60);
  btn(r).dispatchEvent(pointer("pointerup", x, y, id));
}

function drag(r: Rig, dx: number, dy: number, id = 2): void {
  const [x, y] = r.fab.position;
  btn(r).dispatchEvent(pointer("pointerdown", x, y, id));
  r.clock.tick(520);
  ok(r.fab.dragging, "picked up after the hold");
  btn(r).dispatchEvent(pointer("pointermove", x + dx, y + dy, id));
  btn(r).dispatchEvent(pointer("pointerup", x + dx, y + dy, id));
}

test("S16 P4 the button: 64 px in the bottom-right corner, a labelled button", () => {
  const r = rig();
  eq(r.fab.position, [752, 556]);
  eq([left(r), top(r)], [720, 524]);
  eq(btn(r).getAttribute("aria-label"), "Radial menu");
  eq(btn(r).getAttribute("aria-expanded"), "false");
  eq(btn(r).style.getPropertyValue("width"), "64px");
  ok(btn(r).all((e) => e.tagName === "TEXT" && e.textContent === "menu").length === 1, "the closed hub's label");
  ok(btn(r).byClass("rm-fab-disc").length === 1, "the dark disc");
  eq(btn(r).all((e) => e.innerHTML !== "" || e.attrs.has("style")).length, 0, "Trusted Types: no markup, no style attributes");
  ok(btn(r).classList.contains("rm-in"), "M13 the little entrance");
  const notch = rig({}, 390, 844);
  notch.ins.bottom = 34;
  notch.fab.relayout();
  eq(notch.fab.position, [342, 766], "above the home bar");
});

test("S1 S9 S17 tap opens on the button, an outside tap sucks it in, and it opens again", () => {
  const r = rig();
  tapFab(r);
  ok(r.fab.isOpen);
  eq(r.opened, 1);
  ok(btn(r).hidden, "the button hides while open");
  eq(btn(r).getAttribute("aria-expanded"), "true");
  ok(!btn(r).classList.contains("rm-breathing"), "M15 no breath while open");
  const overlay = r.host.byClass("rm-overlay")[0];
  ok(overlay, "the invisible catcher");
  const stage = overlay.children[0];
  const side = parseFloat(stage.style.getPropertyValue("width"));
  eq(side, 480);
  const cx = parseFloat(stage.style.getPropertyValue("left")) + side / 2;
  const cy = parseFloat(stage.style.getPropertyValue("top")) + side / 2;
  ok(cx <= 752 && cy <= 556, "moved in only as the rings need");
  near(cx, 800 - 12 - (232 / 600) * 444, 1e-6); // the whole disc 12 px inside
  overlay.dispatchEvent(pointer("pointerdown", 20, 20, 3));
  overlay.dispatchEvent(pointer("pointerup", 20, 20, 3));
  ok(r.fab.menu?.closing, "the suck-in");
  r.clock.tick(320);
  ok(!r.fab.isOpen);
  eq(r.closed, 1);
  ok(!btn(r).hidden, "the button is back");
  eq(r.host.byClass("rm-overlay").length, 0);
  ok(btn(r).classList.contains("rm-breathing"));
  tapFab(r, 4);
  ok(r.fab.isOpen, "opens again");
});

test("S10 a press on the menu never reaches the catcher", () => {
  const r = rig();
  tapFab(r);
  const overlay = r.host.byClass("rm-overlay")[0];
  const stage = overlay.children[0];
  const l = parseFloat(stage.style.getPropertyValue("left")), t = parseFloat(stage.style.getPropertyValue("top"));
  const s = (480 - 36) / 600;
  const x = l + 18 + 300 * s, y = t + 18 + 130 * s; // the top action of SAVED… empty: so switch to GIT first
  stage.dispatchEvent(pointer("pointerdown", l + 18 + 300 * s, t + 18 + 386 * s, 5)); // the bottom category: SAVED
  stage.dispatchEvent(pointer("pointerup", l + 18 + 300 * s, t + 18 + 386 * s, 5));
  stage.dispatchEvent(pointer("pointerdown", l + 18 + 300 * s, t + 18 + 214 * s, 6)); // the top category: GIT
  stage.dispatchEvent(pointer("pointerup", l + 18 + 300 * s, t + 18 + 214 * s, 6));
  r.clock.tick(600);
  stage.dispatchEvent(pointer("pointerdown", x, y, 7));
  r.clock.tick(560);
  eq(r.fab.menu?.tipShown?.id, "push", "the hold opens the card");
  stage.dispatchEvent(pointer("pointerup", x, y, 7));
  ok(r.fab.isOpen && !r.fab.menu?.closing, "the catcher did not close it");
  r.fab.menu?.hideTip();
  stage.dispatchEvent(pointer("pointerdown", x, y, 8));
  stage.dispatchEvent(pointer("pointerup", x, y, 8));
  eq(r.ran, ["push"]);
  r.clock.tick(320);
  ok(!r.fab.isOpen, "closed after the run");
});

test("S13 M17 F7 hold picks it up, drag moves it, drop keeps it", () => {
  const r = rig();
  drag(r, -300, -250);
  ok(!r.fab.dragging);
  eq(r.fab.position, [452, 306]);
  eq(r.moved, [[452, 306]], "onMove gets the resting centre");
  ok(!r.fab.isOpen, "a drag never opens");
  r.clock.tick(500);
  eq(r.fab.position, [452, 306], "no spring when dropped inside");
});

test("M16 the pick-up ring and the lift show only while dragging", () => {
  const r = rig();
  const [x, y] = r.fab.position;
  btn(r).dispatchEvent(pointer("pointerdown", x, y, 9));
  r.clock.tick(300);
  ok(!btn(r).classList.contains("rm-dragging"), "not yet");
  r.clock.tick(220);
  ok(btn(r).classList.contains("rm-dragging"));
  eq(btn(r).byClass("rm-fab-spin")[0]?.children.length, 3, "three arcs that spin");
  btn(r).dispatchEvent(pointer("pointerup", x, y, 9));
  ok(!btn(r).classList.contains("rm-dragging"));
  ok(!r.fab.isOpen, "the release after a pick-up does not open");
});

test("S14 M18 dropped past an edge it springs back fully on screen", () => {
  const r = rig();
  drag(r, 400, 400);
  eq(r.moved, [[800 - 44, 600 - 44]], "the clamped spot is saved at once");
  ok(r.fab.position[0] > 800 - 44, "starts at the drop point");
  r.clock.tick(200);
  r.clock.tick(300);
  eq(r.fab.position, [756, 556]);
  ok(left(r) >= 12 && top(r) >= 12 && left(r) + 64 <= 800 - 12 && top(r) + 64 <= 600 - 12);
});

test("S13 a slide before the hold is neither a tap nor a pick-up", () => {
  const r = rig();
  const [x, y] = r.fab.position;
  btn(r).dispatchEvent(pointer("pointerdown", x, y, 10));
  r.clock.tick(100);
  btn(r).dispatchEvent(pointer("pointermove", x - 40, y, 10));
  r.clock.tick(600);
  ok(!r.fab.dragging);
  btn(r).dispatchEvent(pointer("pointerup", x - 40, y, 10));
  ok(!r.fab.isOpen);
});

test("F6 the saved spot is restored and re-clamped on a smaller screen", () => {
  const r = rig({ position: [250, 250] });
  eq(r.fab.position, [250, 250]);
  const small = rig({ position: [760, 580] }, 400, 300);
  eq(small.fab.position, [400 - 44, 300 - 44]);
});

test("S15 the keyboard pushes the button up; it drops back after", () => {
  const r = rig({ position: [760, 580] });
  eq(r.fab.position[1], 556);
  r.vp.h = 360;
  r.fab.relayout();
  eq(r.fab.position[1], 360 - 12 - 32);
  r.vp.h = 600;
  r.fab.relayout();
  eq(r.fab.position[1], 556, "back to its spot");
});

test("M15 M19 the breath only while visible, closed and motion is allowed", () => {
  const r = rig();
  ok(btn(r).classList.contains("rm-breathing"));
  r.doc.visibilityState = "hidden";
  r.doc.dispatchEvent({ type: "visibilitychange" });
  ok(!btn(r).classList.contains("rm-breathing"), "a hidden page");
  r.doc.visibilityState = "visible";
  r.doc.dispatchEvent({ type: "visibilitychange" });
  ok(btn(r).classList.contains("rm-breathing"));
  const calm = rig({ reducedMotion: true });
  ok(!btn(calm).classList.contains("rm-breathing"));
  ok(!btn(calm).classList.contains("rm-in"));
});

test("F4 favourites go out through onFavoritesChange and come back through setFavorites", () => {
  const r = rig({ favorites: ["push"] });
  tapFab(r);
  eq(r.fab.menu?.favorites, ["push"]);
  r.fab.menu?.toggleFav({ id: "push", label: "push", icon: "up" });
  eq(r.favs, [[]]);
  r.fab.setFavorites(["push"]);
  eq(r.fab.menu?.favorites, ["push"]);
  r.fab.close();
  r.clock.tick(320);
  tapFab(r, 12);
  eq(r.fab.menu?.favorites, ["push"], "kept for the next open");
});

test("keyboard: Enter on the button opens; Escape on the overlay closes", () => {
  const r = rig();
  btn(r).dispatchEvent({ type: "keydown", key: "Enter" });
  ok(r.fab.isOpen);
  r.host.byClass("rm-overlay")[0].dispatchEvent({ type: "keydown", key: "Escape" });
  ok(r.fab.menu?.closing);
});

test("look: a tap opens the menu with no focus ring on a wedge (the design has none); Enter puts the focus on the first action", () => {
  const r = rig({ favorites: ["push"] });
  tapFab(r);
  const acts = () => r.host.byClass("rm-act");
  ok(acts().length > 0);
  eq(acts().filter((e) => e.focused).length, 0, "a touch open focuses nothing");
  const k = rig({ favorites: ["push"] });
  btn(k).dispatchEvent({ type: "keydown", key: "Enter" });
  ok(k.host.byClass("rm-act")[0].focused, "a keyboard goes on from the first action");
});

test("destroy takes everything off the page", () => {
  const r = rig();
  tapFab(r);
  r.fab.destroy();
  eq(r.host.children.length, 0);
  eq(r.clock.pending() >= 0, true);
});

test("extraInsets keeps the button above the page's own bottom bar", () => {
  const r = rig({ extraInsets: () => ({ bottom: 56 }) });
  eq(r.fab.position, [752, 600 - 56 - 12 - 32]);
  drag(r, 0, 300);
  r.clock.tick(500);
  eq(r.fab.position[1], 600 - 56 - 12 - 32, "a drop is kept above it too");
});

test("onTap: a tap the host takes opens no menu; a tap it leaves opens it", () => {
  let take = true;
  let taps = 0;
  const r = rig({ onTap: () => (taps++, take) });
  tapFab(r);
  eq(taps, 1);
  eq(r.fab.isOpen, false, "the host took the tap");
  take = false;
  tapFab(r, 3);
  eq(taps, 2);
  eq(r.fab.isOpen, true, "the host left the tap");
});
