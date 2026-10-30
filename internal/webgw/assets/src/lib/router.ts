// The router: the route lives in the hash; moving pushes a history entry
// so the phone's back button works (a card sheet closes first, then the
// view goes back). Replacing is for redirects that should not add a step.
import { signal } from "@preact/signals";
import { formatRoute, parseHash, type Route, type Tab } from "../state/route";

export const route = signal<Route>(parseHash(location.hash));

/** go moves to r; replace keeps the history length (redirects). */
export function go(r: Route, replace = false): void {
  const hash = formatRoute(r);
  if (hash !== location.hash) {
    if (replace) history.replaceState(null, "", hash);
    else history.pushState(null, "", hash);
  }
  route.value = r;
}

/** goTab opens a tab at its top level. */
export function goTab(tab: Tab): void {
  go({ tab, rest: "", query: {} });
}

/** back steps back in history, or to the tab's top when there is no step. */
export function back(): void {
  if (history.length > 1) history.back();
  else go({ tab: route.value.tab, rest: "", query: {} }, true);
}

function sync(): void {
  route.value = parseHash(location.hash, route.value.tab);
}

addEventListener("popstate", sync);
addEventListener("hashchange", sync);
