// Entry of the phone chunk (phone.js, cmd/webbuild): phone views that
// the first files do not carry. app.js loads it on first use
// (lib/lazy.ts); it shares app.js's modules through app-chunk.js and
// imports no style sheet (the phone's styles are in app.css).
export { AllToolsView } from "./home/AllToolsView";
