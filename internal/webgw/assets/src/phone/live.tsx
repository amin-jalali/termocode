// The phone states on the live link (MOBUX-21). The only phone file that
// reads the session: the phone's lazy chunk (MOB-4) calls startPhoneConn
// once and wraps its screen in ConnScope; the kit never imports this.
import { h, type ComponentChildren } from "preact";
import { effect, signal } from "@preact/signals";
import { useNow } from "../lib/time";
import { link } from "../session";
import { t } from "../strings";
import { finalText, limitText } from "../ui/OfflineBar";
import { barShown, canAnswer, lastSeen, phoneConn, phoneOnline, type ConnInput } from "./conn";
import { ConnLine } from "./States";

/** net is the phone's own network (navigator.onLine and its events). */
const net = signal(typeof navigator === "undefined" ? true : navigator.onLine !== false);

let started = false;

/** startPhoneConn keeps phoneOnline in step with the link and the
 * network; on the network's return it tries the link at once. Once. */
export function startPhoneConn(): void {
  if (started) return;
  started = true;
  if (typeof window !== "undefined") {
    window.addEventListener("online", () => {
      net.value = true;
      link.tryNow();
    });
    window.addEventListener("offline", () => {
      net.value = false;
    });
  }
  // online/offline do not depend on the clock, only asleep does.
  effect(() => {
    phoneOnline.value = canAnswer(phoneConn({ link: link.state.value, downSince: link.downSince.value, now: Date.now(), net: net.value }));
  });
}

function input(now: number): ConnInput {
  return { link: link.state.value, downSince: link.downSince.value, now, net: net.value };
}

/** ConnBar is the bar under the phone's top bar for the live link. */
export function ConnBar() {
  const ticking = link.state.value === "offline";
  const now = useNow(ticking, 1000);
  const i = input(now);
  const c = phoneConn(i);
  if (!barShown(c, i)) return null;
  if (c === "final") {
    const f = finalText(link.finalCode.value);
    const main = f.limit ? limitText(f.limit, link.meta.value) : t(f.main);
    return <ConnLine conn="final" text={f.sub ? main + " " + t(f.sub) : main} />;
  }
  if (c === "asleep" && i.downSince !== null) {
    const seen = lastSeen(i.downSince, now);
    return (
      <ConnLine
        conn="asleep"
        text={t(seen.day ? "ph.asleep_day" : "ph.conn_asleep", { time: seen.time })}
        spoken={t("ph.asleep_sr", { time: seen.time })}
        onTry={() => link.tryNow()}
      />
    );
  }
  return <ConnLine conn="offline" text={t("ph.conn_offline")} spoken={t("ph.offline_sr")} onTry={net.value ? () => link.tryNow() : undefined} />;
}

/** ConnScope is a phone screen with the bar on top; offline, every
 * .ph-answer inside it is greyed and takes no taps (phone.css). */
export function ConnScope({ children }: { children: ComponentChildren }) {
  const off = !phoneOnline.value;
  return (
    <div class={`ph-scope${off ? " is-offline" : ""}`}>
      <ConnBar />
      {children}
    </div>
  );
}
