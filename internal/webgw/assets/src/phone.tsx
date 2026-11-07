// Entry of the phone views chunk (phone.js, cmd/webbuild, MOB-14 on):
// the views only the phone layout shows. app.js loads it when one opens
// (lib/lazy.ts); it shares app.js's modules through app-chunk.js. Its
// strings (strings.phone.ts) are English only and live in this file only.
import "./strings.phone";
export { ReviewView } from "./views/phone/Review";
