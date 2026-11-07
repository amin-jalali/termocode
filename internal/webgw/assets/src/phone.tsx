// The phone's own views (mobile app, MOB / MOBUX), built into phone.js:
// app.js loads it with a dynamic import (lib/lazy.ts) the first time a
// phone view opens, so the page's first files never carry them. It
// shares app.js's modules through app-chunk.js. Its strings
// (strings.phone.ts, English only) join the page's when it loads.
import { addStrings } from "./strings";
import { SP } from "./strings.phone";

addStrings(SP);

export { ProjectPage } from "./views/phone/ProjectPage";
