// Line blame on the desktop editor: who last changed each line of the
// saved file (git.blame, the laptop's git). The cursor's line shows the
// author, how long ago and the subject at its end, faded; hovering there
// shows the commit. On by default; Git: Toggle Line Blame turns it off.
import { signal } from "@preact/signals";
import type { GitBlame } from "../../../../webproto/proto";
import type { FrameBlame } from "../monaco/protocol";
import { lang, t } from "../strings";

/** blameOn: the editor shows line blame. */
export const blameOn = signal(true);

const UNITS: readonly [Intl.RelativeTimeFormatUnit, number][] = [
  ["year", 365 * 86400],
  ["month", 30 * 86400],
  ["week", 7 * 86400],
  ["day", 86400],
  ["hour", 3600],
  ["minute", 60],
];

/** ago says how long ago a Unix ms time was, in words ("3 weeks ago"). */
export function ago(when: number, now: number): string {
  const s = Math.round((when - now) / 1000);
  const f = new Intl.RelativeTimeFormat(lang.peek() === "fa" ? "fa" : "en", { numeric: "auto" });
  for (const [u, n] of UNITS) if (Math.abs(s) >= n) return f.format(Math.round(s / n), u);
  return f.format(0, "minute");
}

/** blameRows turns a git.blame answer into the editor's rows. */
export function blameRows(b: GitBlame, now = Date.now()): (FrameBlame | null)[] {
  const cs = Array.isArray(b?.commits) ? b.commits : [];
  const made = cs.map((c): FrameBlame => {
    if (c.uncommitted) return { text: t("bl.you_now"), author: t("bl.you"), when: "", subject: t("bl.uncommitted"), hash: "" };
    const when = c.when ? ago(c.when, now) : "";
    return { text: t("bl.line", { author: c.author ?? "", when, subject: c.subject ?? "" }), author: c.author ?? "", when, subject: c.subject ?? "", hash: c.hash ?? "" };
  });
  return (Array.isArray(b?.lines) ? b.lines : []).map((i) => made[i] ?? null);
}
