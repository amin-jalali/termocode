// home.js (mobile plan Q4): the phone's own views, built beside app.js and
// loaded by it (lib/lazy.ts) only on a phone, when one opens; the desktop
// never requests it. It shares Preact and the session with the page
// through app-chunk.js. This phone (MOBUX-20) and the Home (MOB-4, which
// gets its host through init) and All tools (MOBUX-5).
export { ThisPhone } from "./views/phone/ThisPhone";
export { Home, init } from "./home/main";
export { AllToolsView } from "./home/AllToolsView";
// Review in chat (MOB-14, once its own phone.js); its English-only
// strings come with it.
import "./strings.phone";
export { ReviewView } from "./views/phone/Review";
// The live board (MOB-18, once its own board.js).
export { BoardView } from "./views/board/main";
