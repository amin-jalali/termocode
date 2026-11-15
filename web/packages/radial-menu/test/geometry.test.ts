import { ACT_ICON_R, C, CAT_ICON_R, GAP_RING, HUB, MAX_ITEMS, PROG_R, RI1, RI2, RO1, RO2, actCentre, arcLength, catCentre, clampFab, defaultFab, fromDesign, hitTest, placeMenu, placeTip, polar, ringRadius, sectorPath, stageSide, toDesign } from "../src/geometry";
import { eq, near, ok, test } from "./t";

test("G1-G4 G14 the design's rings: centre 300, 56..116, 128..212, hub 50, gap ring 122", () => {
  eq([C, RI1, RO1, RI2, RO2, HUB, GAP_RING], [300, 56, 116, 128, 212, 50, 122]);
  eq([CAT_ICON_R, ACT_ICON_R, PROG_R], [86, 170, 207]);
  eq(MAX_ITEMS, 8);
});

test("G5 polar: 0° is the top, clockwise", () => {
  const top = polar(100, 0), right = polar(100, 90), bottom = polar(100, 180);
  near(top[0], 300);
  near(top[1], 200);
  near(right[0], 400);
  near(right[1], 300);
  near(bottom[1], 400);
});

test("G6 sectorPath: outer arc clockwise, inner back, large arc past 180°", () => {
  const small = sectorPath(-45, 45, 128, 212);
  ok(small.startsWith("M"), small);
  ok(small.includes("A212,212 0 0 1"), small);
  ok(small.includes("A128,128 0 0 0"), small);
  ok(small.endsWith("Z"));
  ok(sectorPath(0, 200, 128, 212).includes("A212,212 0 1 1"), "large arc flag");
});

test("G7 G15 the progress arc's length", () => {
  near(arcLength(0, 180, 207), Math.PI * 207);
});

test("G8 G11 categories start at the bottom, actions at the top", () => {
  eq(catCentre(0, 6), 180);
  eq(catCentre(1, 6), 240);
  eq(actCentre(0, 4), 0);
  eq(actCentre(1, 4), 90);
});

test("G16 hit test: hub, closed menu, both rings, the gaps", () => {
  eq(hitTest(300, 300, false, 6, 4), { kind: "hub" });
  eq(hitTest(300, 300 + 86, false, 6, 4), null, "a closed menu has only its hub");
  eq(hitTest(300, 300 + 86, true, 6, 4), { kind: "cat", i: 0 }, "the first category is at the bottom");
  eq(hitTest(300 - 86, 300, true, 4, 4), { kind: "cat", i: 1 }, "then clockwise: left");
  eq(hitTest(300, 300 - 170, true, 6, 4), { kind: "act", i: 0 }, "the first action is at the top");
  eq(hitTest(300 + 170, 300, true, 6, 4), { kind: "act", i: 1 });
  eq(hitTest(300, 300 - 122, true, 6, 4), null, "between the rings");
  eq(hitTest(300, 300 - 230, true, 6, 4), null, "past the outer ring");
  eq(hitTest(300, 300 - 170, true, 6, 0), null, "an empty ring");
});

test("G17 G18 the box: at most 480, 12 from each edge; the rings' reach", () => {
  eq(stageSide(390, 844), 366, "phone");
  eq(stageSide(1024, 768), 480, "tablet");
  eq(stageSide(844, 390), 366, "landscape phone");
  near(ringRadius(480), (220 / 600) * 444);
});

test("G19 G20 the menu opens on the button and moves in only as far as the rings need", () => {
  const free = placeMenu([500, 380], 1024, 768);
  eq([free.cx, free.cy], [500, 380], "room around it: centred on the button");
  near(free.ox, 0.5);
  const corner = placeMenu([390 - 48, 844 - 44], 390, 844, { top: 20, right: 0, bottom: 34, left: 0 });
  const rr = ringRadius(366);
  near(corner.cx, 390 - 12 - rr, 1e-9, "kept inside on the right");
  near(corner.cy, 844 - 34 - 12 - rr, 1e-9, "kept above the home bar");
  ok(corner.ox > 0.5 && corner.oy > 0.5, "it grows from the button's corner");
  const top = placeMenu([200, 10], 390, 844, { top: 47, right: 0, bottom: 0, left: 0 });
  near(top.cy, 47 + 12 + rr, 1e-9, "below the notch");
  const none = placeMenu(null, 400, 800);
  eq([none.cx, none.cy], [200, 400]);
});

test("design ↔ page coordinates round trip", () => {
  const box = { left: 10, top: 20, side: 336 };
  const [x, y] = fromDesign(300, 130, box);
  const [a, b] = toDesign(x, y, box);
  near(a, 300);
  near(b, 130);
  near(fromDesign(0, 0, box)[0], 28, 1e-9, "18 px padding");
});

test("S16 S14 the button's default corner and the clamp", () => {
  eq(defaultFab(800, 600), [752, 556], "W-16-32, H-12-32");
  eq(defaultFab(390, 844, { top: 47, right: 0, bottom: 34, left: 0 }), [342, 766], "above the home bar");
  eq(clampFab([900, 900], 800, 600), [800 - 44, 600 - 44], "12 px inside");
  eq(clampFab([-50, -50], 800, 600, { top: 30, right: 0, bottom: 0, left: 0 }), [44, 74]);
  eq(clampFab([400, 300], 800, 600), [400, 300], "inside stays");
  const side = { top: 0, right: 47, bottom: 21, left: 47 };
  eq(defaultFab(844, 390, side), [844 - 47 - 16 - 32, 390 - 21 - 12 - 32], "landscape: clear of the notch side");
  eq(clampFab([5, 100], 844, 390, side), [47 + 44, 100]);
  eq(clampFab([840, 100], 844, 390, side), [844 - 47 - 44, 100]);
});

test("D7 the card: above, flipped below without room, never off screen", () => {
  const up = placeTip(200, 400, 178, 100, 390, 844, 8, 8);
  eq([up.left, up.top, up.below], [111, 286, false]);
  near(up.arrow, 89);
  const down = placeTip(200, 60, 178, 100, 390, 844, 8, 8);
  eq([down.top, down.below], [74, true]);
  const edge = placeTip(10, 400, 178, 100, 390, 844, 8, 8);
  eq(edge.left, 8, "kept 8 px in on the left");
  ok(edge.arrow >= 12 && edge.arrow < 40, "the arrow still points at the press");
  const right = placeTip(388, 400, 178, 100, 390, 844, 8, 8);
  eq(right.left, 390 - 8 - 178);
  const low = placeTip(200, 840, 178, 100, 390, 300, 8, 8);
  ok(low.top + 100 <= 300 - 8, "kept above the bottom");
});
