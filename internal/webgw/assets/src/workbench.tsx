// Entry of the desktop workbench chunk (workbench.js, cmd/webbuild): the
// layout and the views only a wide screen shows. app.js loads it on a
// desktop only (lib/lazy.ts); it shares app.js's modules through
// app-chunk.js.
export { StatusBar, Workbench } from "./ui/Workbench";
