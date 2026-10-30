// Keep the screen on while a run streams (WEB-36): opt-in, from the
// terminal's ⓘ sheet. The lock is held only while the user wants it, the
// shell on screen runs something and the page is visible; the browser
// drops it when the page hides, so it is asked for again on return.
import { useEffect } from "preact/hooks";

interface Sentinel {
  release(): Promise<void>;
}
interface WakeLockAPI {
  request(type: "screen"): Promise<Sentinel>;
}

function api(): WakeLockAPI | undefined {
  return (navigator as Navigator & { wakeLock?: WakeLockAPI }).wakeLock;
}

/** canKeepOn reports whether this browser can keep the screen on. */
export function canKeepOn(): boolean {
  return api() !== undefined;
}

/** useWakeLock holds the screen lock while want is true and the page shows. */
export function useWakeLock(want: boolean): void {
  useEffect(() => {
    const wl = api();
    if (!want || !wl) return;
    let held: Sentinel | null = null;
    let asking = false;
    let done = false;
    const drop = () => {
      const s = held;
      held = null;
      s?.release().catch(() => undefined);
    };
    const sync = () => {
      if (document.hidden) {
        drop();
        return;
      }
      if (held || asking) return;
      asking = true;
      wl.request("screen").then(
        (s) => {
          asking = false;
          if (done || document.hidden) s.release().catch(() => undefined);
          else held = s;
        },
        () => (asking = false), // refused (battery saver, no permission): the screen may sleep
      );
    };
    document.addEventListener("visibilitychange", sync);
    sync();
    return () => {
      done = true;
      document.removeEventListener("visibilitychange", sync);
      drop();
    };
  }, [want]);
}
