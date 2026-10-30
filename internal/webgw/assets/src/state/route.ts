// Routes of the page, all in the URL hash so the gateway serves one path
// (web UX 4.4): #/chat, #/files/<path>, #/term/<buf>, #/git, #/git/<path>,
// #/more, #/more/search?q=, #/card/<id>. The hash never carries a token.
// Pure: parse and format only; lib/router.ts talks to history.

export const TABS = ["chat", "files", "term", "git", "more"] as const;
export type Tab = (typeof TABS)[number];

export interface Route {
  tab: Tab;
  /** rest is the decoded path after the tab: a file path, a buffer, a sub view. */
  rest: string;
  /** query holds the decoded ?a=b pairs of the hash. */
  query: Record<string, string>;
  /** card is the id of an open card sheet (#/card/<id>), shown over tab. */
  card?: string;
}

export const DEFAULT_ROUTE: Route = { tab: "chat", rest: "", query: {} };

function decode(s: string): string | null {
  try {
    return decodeURIComponent(s);
  } catch {
    return null;
  }
}

/** parseHash reads a location hash; anything unknown is the default route. */
export function parseHash(hash: string, under: Tab = "chat"): Route {
  const m = /^#\/([a-z]+)(?:\/([^?]*))?(?:\?(.*))?$/.exec(hash);
  if (!m) return { ...DEFAULT_ROUTE, tab: under };
  const query: Record<string, string> = {};
  for (const pair of (m[3] ?? "").split("&")) {
    if (!pair) continue;
    const [k, v = ""] = pair.split("=");
    const dk = decode(k.replace(/\+/g, " ")), dv = decode(v.replace(/\+/g, " "));
    if (dk !== null && dv !== null) query[dk] = dv;
  }
  const rest = decode(m[2] ?? "");
  if (rest === null || rest.split("/").some((seg) => seg === ".." || seg === ".")) {
    return { ...DEFAULT_ROUTE, tab: under };
  }
  if (m[1] === "card") {
    return rest ? { tab: under, rest: "", query: {}, card: rest } : { ...DEFAULT_ROUTE, tab: under };
  }
  const tab = (TABS as readonly string[]).includes(m[1]) ? (m[1] as Tab) : null;
  if (!tab) return { ...DEFAULT_ROUTE, tab: under };
  return { tab, rest, query };
}

/** formatRoute writes a route as a hash; parseHash(formatRoute(r)) gives r back. */
export function formatRoute(r: Route): string {
  if (r.card) return "#/card/" + encodeURIComponent(r.card);
  let h = "#/" + r.tab;
  if (r.rest) h += "/" + r.rest.split("/").map(encodeURIComponent).join("/");
  const q = Object.keys(r.query)
    .sort()
    .map((k) => encodeURIComponent(k) + "=" + encodeURIComponent(r.query[k]))
    .join("&");
  return q ? h + "?" + q : h;
}
