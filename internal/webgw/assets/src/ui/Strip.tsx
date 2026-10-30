// The status strip: the one thing every screenshot shows (web UX 2).
// Mono facts only, always LTR: the termocode pill, branch, dirty count,
// the ✦ AI state, the web chip and the link's RTT dot.
import { h } from "preact";
import { setPref, prefs } from "../lib/prefs";
import { link, status } from "../session";
import type { StatusView } from "../state/status";
import { t } from "../strings";
import { IconBranch, IconMoon, IconSun } from "./icons";

/** rttLevel maps an RTT to the dot colour (web UX 5.1). */
export function rttLevel(ms: number | null): "none" | "good" | "slow" | "bad" {
  if (ms === null) return "none";
  if (ms <= 150) return "good";
  if (ms <= 500) return "slow";
  return "bad";
}

/** Comet is the TUI's busy comet: a short bright run moving along a track. */
export function Comet() {
  return (
    <span class="comet" aria-hidden="true">
      {[0, 1, 2, 3, 4, 5].map((i) => (
        <span key={i} class={`comet-cell c${i}`}>─</span>
      ))}
    </span>
  );
}

function ThemeToggle() {
  const p = prefs.value;
  const dark =
    p.theme === "dark" || (p.theme === "system" && !matchMedia("(prefers-color-scheme: light)").matches);
  return (
    <button
      class="strip-btn"
      type="button"
      aria-label={t("theme")}
      title={t("theme")}
      onClick={() => setPref("theme", dark ? "light" : "dark")}
    >
      {dark ? <IconSun /> : <IconMoon />}
    </button>
  );
}

/** Strip draws the live status. */
export function Strip() {
  return <StripView s={status.value} online={link.state.value === "online"} rtt={link.rtt.value} />;
}

/** StripView draws a given status (the kit shows every state with it). */
export function StripView({ s, online, rtt }: { s: StatusView; online: boolean; rtt: number | null }) {
  const level = online ? rttLevel(rtt) : "none";
  const state = online ? "online" : "offline";
  const ai = s.cardsWaiting > 0 ? "waiting" : s.aiBusy ? "working" : "idle";
  const tint = s.aiBusy ? "ai" : s.dirty > 0 ? "dirty" : "calm";
  return (
    <header class="strip" dir="ltr">
      <span class={`brand tint-${tint}`}>termocode</span>
      {s.branch && (
        <span class="strip-item branch" title={s.branch}>
          <IconBranch />
          <span class="trunc">{s.branch}</span>
        </span>
      )}
      {s.dirty > 0 && <span class="strip-item dirty">● {s.dirty}</span>}
      <span class={`strip-item ai ai-${ai}`}>
        <span class="star" aria-hidden="true">✦</span>
        <span>{t(ai === "waiting" ? "strip.ai_waiting" : ai === "working" ? "strip.ai_working" : "strip.ai_idle")}</span>
        {ai === "working" && <Comet />}
      </span>
      <span class="strip-gap" />
      {s.webClients > 0 && (
        <span class="strip-item web" title={t("strip.web", { n: s.webClients })}>
          ◉ <span class="web-label">{t("strip.web", { n: s.webClients })}</span>
          <span class="web-n">{s.webClients}</span>
        </span>
      )}
      {level === "slow" || level === "bad" ? (
        <span class="strip-item rtt-text">{t("rtt.slow", { ms: rtt ?? 0 })}</span>
      ) : null}
      <span
        class={`rtt rtt-${level}`}
        role="img"
        aria-label={rtt !== null && state === "online" ? t("rtt.label", { ms: rtt }) : t("offline.input")}
        title={rtt !== null && state === "online" ? t("rtt.label", { ms: rtt }) : t("offline.input")}
      />
      <span class="desk-only">
        <ThemeToggle />
      </span>
    </header>
  );
}
