// A short haptic when a card is answered (MOB-6, MOBUX-12): the phone
// app's bridge when the page runs inside it (the Capacitor Haptics
// plugin), else the browser's vibrate where there is one (Android
// Chrome); nothing on a desktop or an iPhone browser. It never throws and
// never waits: a missing or failing bridge is no haptic, not an error.

type Impact = "light" | "medium";

interface HapticsBridge {
  impact?: (o: { style: string }) => unknown;
}

function bridge(): HapticsBridge | undefined {
  const w = globalThis as { Capacitor?: { Plugins?: { Haptics?: HapticsBridge } } };
  return w.Capacitor?.Plugins?.Haptics;
}

/** haptic gives one short tap: light for a safe answer, medium for a risky one. */
export function haptic(kind: Impact = "light"): void {
  try {
    const b = bridge();
    if (b?.impact) {
      const p = b.impact({ style: kind === "medium" ? "MEDIUM" : "LIGHT" });
      if (p instanceof Promise) p.catch(() => undefined);
      return;
    }
    if (typeof navigator !== "undefined" && typeof navigator.vibrate === "function") navigator.vibrate(kind === "medium" ? 20 : 10);
  } catch {
    // no haptic: nothing to tell the user
  }
}
