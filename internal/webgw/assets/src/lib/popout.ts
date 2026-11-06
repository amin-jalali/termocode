// Pop out (WEBP-13): one terminal in its own browser window, a live view
// of the same laptop shell. window.open without noopener copies this
// tab's session storage (net/auth.ts keeps the token there) into the new
// one, so it opens on the same session (no second sign-in); the new
// window drops its opener at once. Closing it ends only its connection:
// the shell stays on the laptop.
import { formatRoute, type Route } from "../state/route";

/** soloBuf is the shell a popped-out tab shows (#/term/<buf>?solo=1), or null. */
export function soloBuf(r: Route): number | null {
  return r.tab === "term" && r.query.solo === "1" && /^\d+$/.test(r.rest) ? Number(r.rest) : null;
}

/** popOut opens buf in its own window (one per shell; opening it again
 * brings that window back), the size of this page so it shows the same
 * terminal view; false when the browser blocked it. */
export function popOut(buf: number): boolean {
  const url = location.pathname + formatRoute({ tab: "term", rest: String(buf), query: { solo: "1" } });
  const size = `popup,width=${Math.round(innerWidth)},height=${Math.round(innerHeight)}`;
  return window.open(url, "termocode-term-" + buf, size) !== null;
}
