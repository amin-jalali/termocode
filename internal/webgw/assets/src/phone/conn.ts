// Phone connection state (MOBUX-21), pure: no session here, so the kit
// and the base components can read it. phone/live.tsx feeds phoneOnline
// from the live link and the phone's network.
//   online      the link is up: answers can be sent
//   connecting  the first connect is still running
//   offline     the phone has no network, or the link dropped a moment ago
//   asleep      the phone has a network but the laptop has not answered
//               for ASLEEP_AFTER_MS: it is asleep or off; show last seen
//   final       the session is over (ui/OfflineBar finalText says why)
// Never queue a send or an answer offline (plan: Offline / reconnect).
import { signal } from "@preact/signals";
import type { LinkState } from "../net/link";

export type PhoneConn = "online" | "connecting" | "offline" | "asleep" | "final";

/** The bar shows after a 1 s drop, as on the web panel (a blip is quiet). */
export const SHOW_AFTER_MS = 1000;
/** The laptop counts as asleep after 30 s out of reach with a network. */
export const ASLEEP_AFTER_MS = 30_000;

export interface ConnInput {
  link: LinkState;
  /** downSince is when the link went down (ms), null while up. */
  downSince: number | null;
  now: number;
  /** net is navigator.onLine: false means the phone itself is offline. */
  net: boolean;
}

export function phoneConn(i: ConnInput): PhoneConn {
  if (i.link === "final") return "final";
  if (!i.net) return "offline";
  if (i.link === "online") return "online";
  if (i.link === "connecting") return "connecting";
  if (i.downSince !== null && i.now - i.downSince >= ASLEEP_AFTER_MS) return "asleep";
  return "offline";
}

/** canAnswer: only a live link with a network takes an answer or a send. */
export function canAnswer(c: PhoneConn): boolean {
  return c === "online";
}

/** barShown hides the bar during a short blip and while connecting. */
export function barShown(c: PhoneConn, i: ConnInput): boolean {
  if (c === "online" || c === "connecting") return false;
  if (c === "offline" && i.net && i.downSince !== null && i.now - i.downSince < SHOW_AFTER_MS) return false;
  return true;
}

const DAY_MS = 24 * 60 * 60 * 1000;

/** lastSeen is when the laptop was last heard: a clock time today
 * ("10:42"), a day and time before ("Nov 6, 10:42"); day says which. */
export function lastSeen(at: number, now: number): { time: string; day: boolean } {
  const d = new Date(at);
  const time = d.toLocaleTimeString("en", { hour: "2-digit", minute: "2-digit" });
  const sameDay = new Date(now).toDateString() === d.toDateString() && now - at < DAY_MS;
  if (sameDay) return { time, day: false };
  return { time: d.toLocaleDateString("en", { month: "short", day: "numeric" }) + ", " + time, day: true };
}

/** phoneOnline is canAnswer for the live link (phone/live.tsx sets it);
 * the composer and the sheets grey their answers when it is false. */
export const phoneOnline = signal(true);
