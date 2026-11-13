// home.js (mobile plan Q4): the phone's own views, built beside app.js and
// loaded by it (lib/lazy.ts) only on a phone, when one opens; the desktop
// never requests it. It shares Preact and the session with the page
// through app-chunk.js. This phone (MOBUX-20) and the Home (MOB-4, which
// gets its host through init) and All tools (MOBUX-5).
export { ThisPhone } from "./views/phone/ThisPhone";
export { pushOn, pushOff } from "./lib/push";
export { Home, init } from "./home/main";
export { AllToolsView } from "./home/AllToolsView";
// Review in chat (MOB-14, once its own phone.js); its English-only
// strings come with it.
import "./strings.phone";
export { ReviewView } from "./views/phone/Review";
// The live board (MOB-18, once its own board.js).
export { BoardView } from "./views/board/main";
// The phone's projects list and project page (MOBUX-6/7, once phone.js).
export { ProjectPage, ProjectsPage } from "./views/phone/ProjectPage";
// More › Containers and Databases with their hosts tree and a database
// read's questions (app.js had them before).
export { ContainersView } from "./views/more/ContainersView";
export { DBView } from "./views/more/DBView";
// The phone chat's look (the desktop's, phone-sized): Chat.tsx loads
// home.js on a phone for it.
import { phoneLook } from "./phone/chatlook";
phoneLook();
