// The termocode app's phone layout (owner 2026-11-19, on the Galaxy Tab
// S11: "just like the mobile view"). The app's bridge, put in the page
// before it loads (window.Capacitor.Plugins.TermocodeAppKey, as in
// net/appkey.ts), may say layout "phone". Then the page is the phone's at
// any width: index.html runs this file before app.js, so every width
// check sees a screen under 720 px, the width lib/layout.ts calls a
// phone: matchMedia (lib/layout.ts, lib/workbench.ts' word wrap), the
// stylesheets' @media rules (tokens, shell, components, git, more views,
// and any sheet linked later) and desk.css's link. <html data-layout=
// "phone"> marks it. A browser has no such bridge, so here nothing
// changes; a page script cannot turn it on after the load either. The
// file imports nothing: app.js and its budget do not grow.

/** PHONE_BELOW is lib/layout.ts' phone width: under 720 px. */
const PHONE_BELOW = 720;

type Bridge = { Capacitor?: { Plugins?: { TermocodeAppKey?: { layout?: unknown } } } };

/** narrow is query q on a phone: a min-width of 720 px or more is never
 * true there, a max-width of 720 px or more always; smaller widths are
 * the screen's own. */
function narrow(q: string): string {
  return q.replace(/\(\s*(min|max)-width\s*:\s*([\d.]+)px\s*\)/g, (f, k: string, n: string) =>
    +n < PHONE_BELOW ? f : k === "min" ? "(max-width: 0px)" : "(min-width: 0px)",
  );
}

function fix(rules: CSSRuleList): void {
  for (const r of rules) {
    if (r instanceof CSSMediaRule) {
      const q = narrow(r.media.mediaText);
      if (q !== r.media.mediaText) r.media.mediaText = q;
    }
    if (r instanceof CSSGroupingRule) fix(r.cssRules);
  }
}

function sheet(s: CSSStyleSheet | null): void {
  try {
    if (s) fix(s.cssRules);
  } catch {
    // a sheet the page cannot read has no rules of ours
  }
}

if ((window as unknown as Bridge).Capacitor?.Plugins?.TermocodeAppKey?.layout === "phone") {
  const mm = window.matchMedia.bind(window);
  window.matchMedia = (q: string) => mm(narrow(q));
  for (const l of document.querySelectorAll<HTMLLinkElement>("link[rel=stylesheet][media]")) l.media = narrow(l.media);
  for (const s of document.styleSheets) sheet(s);
  // The chunks' stylesheets (pages.css, gitphone.css, …) come later.
  document.addEventListener("load", (e) => e.target instanceof HTMLLinkElement && sheet(e.target.sheet), true);
  document.documentElement.dataset.layout = "phone";
}
