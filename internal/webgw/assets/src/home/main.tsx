// home.js (MOB-4): the phone's Home, built alone (cmd/webbuild
// homeChunk). The page calls init with its host, then draws Home.
import { setHost } from "./jsx";
import type { HomeHost } from "./api";

export { Home } from "./Home";

export function init(host: HomeHost): void {
  setHost(host);
}
