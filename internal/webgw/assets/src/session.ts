// The page's one store and one link to the hub.
import { computed } from "@preact/signals";
import { token } from "./net/auth";
import { Link } from "./net/link";
import { readStatus } from "./state/status";
import { readTheme } from "./state/theme";
import { Store } from "./state/store";

export const store = new Store();

function wsURL(): string {
  return (location.protocol === "https:" ? "wss:" : "ws:") + "//" + location.host + "/ws";
}

export const link = new Link({ store, url: wsURL(), token });

/** status is the shell's view of the status topic. */
export const status = computed(() => readStatus(store.topic("status").value?.data));

/** live is false while the link is down: inputs that need the laptop lock (web UX P5). */
export const live = link.live;

/** laptopTheme is the laptop's theme from status.theme (WEB-UX-8), or null. */
export const laptopTheme = computed(() => {
  const d = store.topic("status").value?.data;
  return readTheme(typeof d === "object" && d !== null ? (d as Record<string, unknown>).theme : undefined);
});
