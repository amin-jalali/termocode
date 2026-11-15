import { BLOOM_WINDOW, DUR, breathAt, breathFrame, collapseFrame, cubic, easeInCubic, easeOutBack, easeOutCubic, entranceFrame, flashScale, holdScale, menuIn, petalFrame, pulseFrame, ringsFrame, softBack, sparkFrame, springAt } from "../src/motion";
import { eq, near, ok, test } from "./t";

test("the design's timings", () => {
  eq(
    [DUR.entrance, DUR.rings, DUR.reveal, DUR.hold, DUR.flash, DUR.pulse, DUR.collapse, DUR.toast, DUR.toastStay, DUR.tip, DUR.tipGuard, DUR.fabIn, DUR.breath, DUR.spin, DUR.spring],
    [400, 380, 560, 520, 240, 600, 300, 200, 1500, 220, 450, 220, 2800, 3000, 460],
  );
  eq(BLOOM_WINDOW, 0.55);
});

test("M3 cubic curves: ends fixed, Flutter's values, softBack overshoots", () => {
  const lin = cubic(0, 0, 1, 1);
  near(lin(0.3), 0.3, 1e-4);
  eq([softBack(0), softBack(1)], [0, 1]);
  let peak = 0;
  for (let t = 0; t <= 1; t += 0.01) peak = Math.max(peak, softBack(t));
  ok(peak > 1.01 && peak < 1.1, "a gentle overshoot: " + peak);
  near(easeOutCubic(0.5), 0.8752, 0.01);
  near(cubic(0.25, 0.1, 0.25, 1)(0.5), 0.8024, 0.002, "CSS ease");
  near(easeInCubic(0.5), 0.1455, 0.01);
  ok(easeOutBack(0.7) > 1, "easeOutBack overshoots");
  for (let t = 0.05; t < 1; t += 0.05) ok(menuIn(t) >= menuIn(t - 0.05), "menuIn rises");
});

test("M1 entrance: fade in, .9 → 1", () => {
  eq(entranceFrame(0), { opacity: 0, scale: 0.9 });
  eq(entranceFrame(1), { opacity: 1, scale: 1 });
  near(entranceFrame(0.5).opacity, 0.5);
});

test("M2 rings: .66 → 1 with the spring, a -7° twist that settles", () => {
  const a = ringsFrame(0), b = ringsFrame(1);
  eq([a.opacity, a.scale, a.twist], [0, 0.66, -7]);
  eq([b.opacity, b.scale, b.twist], [1, 1, 0]);
  ok(ringsFrame(0.5).twist > -7 && ringsFrame(0.5).twist < 0);
});

test("M4 the clockwise bloom: each petal in its own window", () => {
  const n = 6;
  ok(petalFrame(0, n, 0.02).show, "the first petal starts at once");
  ok(!petalFrame(5, n, 0.05).show, "the last one waits its turn");
  const mid = petalFrame(5, n, 0.3);
  ok(mid.show && mid.opacity > 0 && mid.opacity < 1, "later it fades in");
  for (let i = 0; i < n; i++) eq(petalFrame(i, n, 1), { show: true, opacity: 1, scale: 1 });
  ok(petalFrame(0, n, 0.01).scale >= 0.6 && petalFrame(0, n, 0.01).scale < 0.7, "grows from .6");
  // the order is clockwise: at any moment an earlier petal is further on
  for (let v = 0.05; v < 1; v += 0.05) ok(petalFrame(1, n, v).opacity >= petalFrame(4, n, v).opacity, "clockwise at " + v);
});

test("C18 the spark rides the bloom's front and fades", () => {
  const a = sparkFrame(0), b = sparkFrame(1);
  near(a.alpha, 0.62);
  near(b.alpha, 0.12);
  near(b.deg, 360);
  ok(sparkFrame(0.3).deg > 0 && sparkFrame(0.3).deg < 360);
});

test("M8 M9 the held petal swells, a fired petal pops", () => {
  eq(holdScale(0), 1);
  near(holdScale(1), 1.05);
  near(flashScale(0.5), 1.07);
  eq([flashScale(0), flashScale(1)], [1, 1]);
});

test("M6 the suck-in: fade, .5 rad swirl, down to .08", () => {
  eq(collapseFrame(0), { opacity: 1, rotate: 0, scale: 1 });
  const e = collapseFrame(1);
  near(e.opacity, 0);
  near(e.rotate, 0.5);
  near(e.scale, 0.08);
});

test("M10 the favourite pulse", () => {
  eq(pulseFrame(0), { opacity: 0.55, scale: 0.55 });
  const e = pulseFrame(1);
  near(e.opacity, 0);
  near(e.scale, 1.3);
});

test("M14 the breath: 2.8 s each way, eased", () => {
  near(breathAt(0), 0);
  near(breathAt(2800), 1);
  near(breathAt(5600), 0, 1e-9);
  near(breathAt(1400), 0.5, 1e-3);
  const f = breathFrame(1);
  near(f.scale, 1.02);
  near(f.alpha, 0.38);
  near(f.blur, 15);
  near(f.grow, 2);
});

test("M18 the spring back overshoots a little and lands", () => {
  eq(springAt([0, 0], [100, 50], 0), [0, 0]);
  eq(springAt([0, 0], [100, 50], 1), [100, 50]);
  ok(springAt([0, 0], [100, 0], 0.7)[0] > 100, "overshoot");
});
