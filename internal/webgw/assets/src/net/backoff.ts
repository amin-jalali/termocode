// Reconnect delays (web design 1.5): 0.5, 1, 2, 4, 8, then 15 s, each
// ±20 % so many tabs do not knock at the same moment.
export const BACKOFF_MS = [500, 1000, 2000, 4000, 8000, 15000] as const;

/** backoff returns the delay before reconnect attempt n (0-based); r in [0, 1). */
export function backoff(n: number, r: number): number {
  const base = BACKOFF_MS[Math.min(Math.max(n, 0), BACKOFF_MS.length - 1)];
  return Math.round(base * (0.8 + 0.4 * r));
}
