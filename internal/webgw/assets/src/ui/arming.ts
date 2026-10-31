// The arming delay of risky buttons (web UX P1): inert for 600 ms after
// the card appears and again each time it scrolls back into view, so a
// tap meant for something else cannot answer it. A press that starts
// while inert does not count even if it ends after.
import { useEffect, useRef, useState, type MutableRef } from "preact/hooks";

export const ARM_MS = 600;
const TICKS = 3;

export interface Arming {
  armed: boolean;
  /** tick counts 1..3 during arming (shown instead of the fill when motion is reduced). */
  tick: number;
  /** pressOK says whether a click may act: armed now and armed when the
   * press (pointer down, or Enter / Space key down) began. */
  pressOK: () => boolean;
  /** pressBegan goes on the button's onPointerDown and onKeyDown. */
  pressBegan: () => void;
}

export function useArming(target: MutableRef<HTMLElement | null>, ms = ARM_MS): Arming {
  const [armed, setArmed] = useState(false);
  const [tick, setTick] = useState(0);
  const [round, setRound] = useState(0);
  const armedRef = useRef(false);
  const pressStartedArmed = useRef(false);

  useEffect(() => {
    armedRef.current = false;
    setArmed(false);
    setTick(0);
    const step = ms / TICKS;
    const ids = Array.from({ length: TICKS }, (_, i) =>
      setTimeout(() => {
        setTick(i + 1);
        if (i + 1 === TICKS) {
          armedRef.current = true;
          setArmed(true);
        }
      }, step * (i + 1)),
    );
    return () => ids.forEach(clearTimeout);
  }, [round, ms]);

  useEffect(() => {
    const el = target.current;
    if (!el || typeof IntersectionObserver === "undefined") return;
    let seen = true;
    const io = new IntersectionObserver((entries) => {
      const visible = entries.some((e) => e.isIntersecting);
      if (visible && !seen) setRound((r) => r + 1); // back in view: arm again
      if (!visible) {
        armedRef.current = false;
        setArmed(false);
      }
      seen = visible;
    });
    io.observe(el);
    return () => io.disconnect();
  }, [target.current]);

  return {
    armed,
    tick,
    pressOK: () => {
      const ok = armedRef.current && pressStartedArmed.current;
      pressStartedArmed.current = false;
      return ok;
    },
    // A press counts only if the button also looked armed when it began:
    // between the timer and the next paint the ref is already true while
    // the button still shows the inert look, and a press then must not act.
    pressBegan: () => {
      pressStartedArmed.current = armed && armedRef.current;
    },
  };
}
