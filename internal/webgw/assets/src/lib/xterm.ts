// Loads the terminal engine (term.js: xterm.js and its glue, built
// alone) and its stylesheet the first time a terminal opens, on the
// phone and on the desktop; a failed load can be tried again.
import type * as Term from "../xterm/main";

export type TermModule = typeof Term;

let mod: Promise<TermModule> | null = null;

/** loadTerm loads term.js once (again after a failure). */
export function loadTerm(): Promise<TermModule> {
  if (!document.querySelector('link[data-xterm]')) {
    const l = document.createElement("link");
    l.rel = "stylesheet";
    l.href = "/assets/term.css";
    l.dataset.xterm = "";
    document.head.appendChild(l);
  }
  return (mod ??= import("/assets/term.js").catch((e: unknown) => {
    mod = null;
    throw e;
  }));
}
