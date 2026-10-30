// Clocks for the UI: a ticking "now" while something counts down or up.
import { useEffect, useState } from "preact/hooks";

/** useNow re-renders every `every` ms while active and returns Date.now(). */
export function useNow(active: boolean, every = 250): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    setNow(Date.now());
    const id = setInterval(() => setNow(Date.now()), every);
    return () => clearInterval(id);
  }, [active, every]);
  return now;
}

/** useAfter is true once `ms` have passed since `key` last changed. */
export function useAfter(ms: number, key: unknown): boolean {
  const [done, setDone] = useState(false);
  useEffect(() => {
    setDone(false);
    const id = setTimeout(() => setDone(true), ms);
    return () => clearTimeout(id);
  }, [key, ms]);
  return done;
}
