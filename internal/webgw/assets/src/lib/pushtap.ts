// A tap on a push notification (sw.js) reaches the page here: a closed
// page was opened with ?open=cards, an open one gets a message. Either
// way the page opens its cards (main.tsx). Read-only: it sends nothing.
export function watchTap(open: () => void): void {
  navigator.serviceWorker?.addEventListener("message", (e: MessageEvent) => e.data?.open === "cards" && open());
  if (location.search.includes("open=cards")) {
    history.replaceState(null, "", location.pathname + location.hash);
    open();
  }
}
