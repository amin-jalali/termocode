// A short buzz when a card gets its answer on the phone (MOBUX-12): one
// tap for the safe answer, a double tap for the risky one. Phones only;
// a Later, a swipe down or "Approve on laptop" never buzz. A browser
// without vibration (or one that refuses it) simply stays quiet. The
// Haptics switch on This phone turns every buzz off (MOBUX-22); this is
// the one module that may call vibrate (phone_a11y_test.go).
import { layout } from "./layout";
import { prefs } from "./prefs";

export type Buzz = "safe" | "risky";

const patterns: Record<Buzz, number | number[]> = { safe: 12, risky: [12, 40, 12] };

export function haptic(kind: Buzz): void {
  if (!prefs.peek().haptics || layout.peek() !== "phone" || typeof navigator.vibrate !== "function") return;
  try {
    navigator.vibrate(patterns[kind]);
  } catch {
    // no vibration here: nothing to do
  }
}
