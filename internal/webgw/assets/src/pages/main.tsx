// pages.js: the phone's tool pages redrawn from the page sheets (UI
// pages 10, 11), built alone with its stylesheet (pages.css) and loaded
// by the page (lib/pages.ts) on a phone or tablet when one opens, so the
// first files and home.js do not grow. It imports types and pure
// helpers only; the page gives it Preact, the session and its views
// through init (host.ts).
import "./pages.css";
import { setHost, type PagesHost } from "./host";
import { PG } from "./strings";

export type { PagesHost } from "./host";
export { SettingsPage } from "./settings";

/** init takes the page's host, once, before a page draws. */
export function init(host: PagesHost): void {
  setHost(host);
  host.addStrings(PG);
}
