// home.js (mobile plan Q4): the phone's own views, built beside app.js and
// loaded by it (lib/lazy.ts) only on a phone, when one opens; the desktop
// never requests it. It shares Preact and the session with the page
// through app-chunk.js. Today: This phone (MOBUX-20); the Home view
// (MOB-4) joins it here.
export { ThisPhone } from "./views/phone/ThisPhone";
