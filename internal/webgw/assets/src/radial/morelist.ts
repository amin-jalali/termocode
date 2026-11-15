// The More list on a phone or tablet (RM-3), in radial.js: a search box
// on top, the views opened last under Recent, then every view in small
// groups, each row with its icon (the radial menu's icons, so both speak
// one language). Every view a phone has is a row here; a new view that
// is not fails more_coverage_test.go. Drawn with the page's Preact
// (RadialHost): this file imports no value of the page.
import type { VNode } from "preact";
import type { StringKey } from "../strings";
import { ICONS } from "../../../../../web/packages/radial-menu/src/icons";
import type { RadialHost } from "./actions";

/** Row is [sub view, its name (a page string), its icon]. */
type Row = [string, StringKey, string];

/** The list's own words (English, like the phone Home's). */
export const LS = {
  find: "Search the list",
  recent: "Recent",
  found: "Found",
  none: "No view has that name.",
} as const;

/** GROUPS are the More list's groups, in order. */
export const GROUPS: [string, Row[]][] = [
  ["AI", [["new-task", "nt.title", "list"], ["agents", "ai.wt_title", "branch"], ["races", "more.races", "flag"], ["review", "pill.review", "diff"], ["itermoc", "im.title", "set"]]],
  ["Code", [["search", "more.search", "find"], ["projects", "ph.switch_title", "grid"], ["tasks", "more.tasks", "list"], ["testing", "act.testing", "flask"], ["debug", "act.run", "bug"]]],
  ["Runs and workflows", [["runs", "more.runs", "play"], ["workflows", "more.workflows", "flow"], ["board", "more.board", "board"], ["services", "act.services", "set"]]],
  ["Data and infrastructure", [["db", "more.db", "db"], ["containers", "more.containers", "box"], ["kube", "act.kube", "cloud"], ["mcp", "more.mcp", "plug"]]],
  ["This device and the app", [["phone", "more.this_phone", "phone"], ["settings", "more.settings", "gear"], ["tools", "tools.title", "grid"]]],
];

/** RECENT is how many recent views lead the list. */
const RECENT = 4;

/** MoreList draws the list with the page's Preact. */
export function MoreList({ host }: { host: RadialHost }): VNode {
  const { h } = host;
  const [q, setQ] = host.useState("");

  // An icon as SVG elements (no markup: the page's Trusted Types).
  const icon = (name: string): VNode =>
    h(
      "svg",
      { class: "more-ic", viewBox: "-10 -10 20 20", width: 20, height: 20, "aria-hidden": "true" },
      [...(ICONS[name] ?? "").matchAll(/<(\w+)([^>]*?)\/>/g)].map((m) => {
        const a = new Map<string, string>();
        for (const x of m[2].matchAll(/([\w-]+)="([^"]*)"/g)) a.set(x[1], x[2] === "{c}" ? "currentColor" : x[2]);
        return h(m[1], Object.fromEntries(a));
      }),
    );
  const link = ([sub, name, ic]: Row): VNode =>
    h(
      "button",
      {
        type: "button",
        class: "more-link",
        "data-sub": sub,
        key: sub,
        onClick: () => {
          host.setPref("moreRecent", [sub, ...host.prefs.peek().moreRecent.filter((s) => s !== sub)].slice(0, RECENT));
          host.go({ tab: "more", rest: sub, query: {} });
        },
      },
      icon(ic),
      h("span", { class: "more-lb" }, host.t(name)),
    );
  const group = (title: string, rows: Row[], cls = ""): VNode =>
    h("section", { class: "more-group" + cls, "aria-label": title, key: title }, h("h2", { class: "group-title" }, title), h("nav", { class: "more-links" }, rows.map(link)));
  const find = h(
    "div",
    { class: "tools-search" },
    h("input", { class: "search-input more-find", type: "search", dir: "auto", value: q, "aria-label": LS.find, placeholder: LS.find, onInput: (e: Event) => setQ((e.currentTarget as HTMLInputElement).value) }),
  );
  const all = GROUPS.flatMap(([, rs]) => rs);
  const words = q.trim().toLowerCase();
  if (words) {
    const hits = all.filter(([sub, name]) => (host.t(name) + " " + sub).toLowerCase().includes(words));
    return h("div", { class: "more-list" }, find, hits.length ? group(LS.found, hits) : h("p", { class: "empty", role: "status" }, LS.none));
  }
  const recent = host.prefs.value.moreRecent.map((s) => all.find((r) => r[0] === s)).filter((r): r is Row => !!r);
  return h("div", { class: "more-list" }, find, recent.length > 0 && group(LS.recent, recent, " is-recent"), GROUPS.map(([g, rs]) => group(g, rs)));
}
